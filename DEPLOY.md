# Deploying to a DigitalOcean droplet

This guide takes a fresh droplet to a running, HTTPS-enabled copy of the app, deployed automatically from GitHub Actions. It uses the same `docker-compose.yml` as local development; production is a few settings in `.env`.

What you get:

- **Automatic deploys.** Every push to `main` is tested, built into Docker images on GitHub's runners, published to the GitHub Container Registry, and rolled out to the droplet over SSH. The small droplet never compiles anything.
- **PostgreSQL** in its own container, with the data in a Docker volume. A backup is taken before every deploy.
- **HTTPS automatically.** Caddy obtains and renews a Let's Encrypt certificate and redirects HTTP to HTTPS.
- **Secure sessions.** Cookies are `HttpOnly`, `SameSite=Lax` and `Secure` whenever the request arrives over HTTPS.
- **Closed sign-ups.** Once your account exists, nobody else can register.
- **Backup and restore** scripts (`pg_dump`/`pg_restore`), both tested end to end.

> The app stores personal financial data. Treat the droplet, its `.env`, and every backup accordingly.

## 1. Before you start

| You need | Notes |
| --- | --- |
| A droplet | Ubuntu 24.04 LTS. The app is light (a Go binary, Postgres and Caddy), so **1 vCPU and 1–2 GB RAM is plenty**. Add your SSH key when creating it. |
| A domain name | Needed for HTTPS. Create an **A record** (e.g. `wellness.example.com`) pointing at the droplet's IP, and let DNS propagate before the first deploy. |
| This repo on GitHub | The workflow lives in `.github/workflows/deploy.yml`. |

You can run without a domain (plain HTTP on the droplet's IP), but then your password and data cross the internet unencrypted. Don't.

## 2. Lock down the server

SSH in as root once, then create a normal user and a firewall.

```bash
adduser deploy && usermod -aG sudo deploy
rsync --archive --chown=deploy:deploy ~/.ssh /home/deploy      # reuse your SSH key
```

```bash
# firewall: SSH, HTTP, HTTPS (443/udp is HTTP/3)
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 443/udp
sudo ufw enable
```

Also recommended: disable password SSH logins (`PasswordAuthentication no` in `/etc/ssh/sshd_config`), run `sudo apt install unattended-upgrades`, and optionally add a DigitalOcean **Cloud Firewall** allowing only 22 (ideally from your IP), 80 and 443.

> Docker publishes container ports by editing iptables directly, which bypasses `ufw`. That's fine here: only 80 and 443 are published to the world. Postgres is published on `127.0.0.1` only, and the API container isn't published at all.

## 3. Install Docker

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker deploy        # then log out and back in
docker compose version                # confirm the compose plugin is present
```

## 4. Prepare the droplet (once)

The deploy job copies the compose file and scripts for you. All it needs on the droplet is a directory and a `.env`:

```bash
sudo mkdir -p /opt/evergreen && sudo chown deploy:deploy /opt/evergreen
cd /opt/evergreen
umask 077
cat > .env <<EOF
POSTGRES_PASSWORD=$(openssl rand -hex 24)
SITE_ADDRESS=wellness.example.com
HTTP_PORT=80
HTTPS_PORT=443
ALLOW_REGISTRATION=false
EOF
```

- **`POSTGRES_PASSWORD`** is required; the deploy refuses to run without it. It's applied when the database is first created. Keep a copy somewhere safe (a password manager).
- **`ALLOW_REGISTRATION=false`** lets the first account be created and refuses every later one.
- The file stays on the droplet; the pipeline never overwrites it (it only adds `TAG`, `REGISTRY` and `COMPOSE_PROJECT_NAME`).

## 5. Connect GitHub to the droplet

**a. A deploy key.** On your own machine:

```bash
ssh-keygen -t ed25519 -C "evergreen-deploy" -f ./evergreen_deploy -N ""
ssh-copy-id -i ./evergreen_deploy.pub deploy@YOUR_DROPLET_IP     # or append the .pub to ~deploy/.ssh/authorized_keys
```

**b. The droplet's host key** (so GitHub verifies it's talking to your server, not just whoever answers on that IP). Get it from a connection you trust:

```bash
ssh-keyscan -t ed25519 YOUR_DROPLET_IP
# compare the fingerprint with:  ssh deploy@YOUR_DROPLET_IP 'ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub'
```

**c. In the repo: Settings → Secrets and variables → Actions**

| Kind | Name | Value |
| --- | --- | --- |
| Secret | `DROPLET_HOST` | the droplet's IP or hostname |
| Secret | `DROPLET_USER` | `deploy` |
| Secret | `DROPLET_SSH_KEY` | the contents of the private key file `evergreen_deploy` |
| Secret | `DROPLET_SSH_KNOWN_HOSTS` | the line printed by `ssh-keyscan` |
| Variable | `DEPLOY_URL` *(optional)* | `https://wellness.example.com`: enables a post-deploy health check |
| Variable | `DEPLOY_PATH` *(optional)* | defaults to `/opt/evergreen` |

Then delete the local `evergreen_deploy` files.

**d. (Recommended) a `production` environment.** Settings → Environments → New environment → `production`. Add yourself as a *required reviewer* and every deploy waits for your approval; you can also move the secrets into the environment so only deploy jobs can read them.

## 6. Deploy

Push to `main` (or Actions → *CI/CD* → *Run workflow*). The pipeline:

1. **test**: `go vet` and `go test -race` against a real Postgres service, then the frontend type-check and build. Pull requests run only this job.
2. **images**: builds the `api` and `web` images and pushes them to `ghcr.io/<you>/evergreen-api` and `evergreen-web`, tagged with the commit SHA and `latest`.
3. **deploy**: copies `docker-compose.yml` and the scripts to the droplet, then runs `deploy.sh` there, which:
   - takes a database backup (a failed backup stops the deploy),
   - pulls the new images using a short-lived registry token (never stored),
   - restarts the stack and waits until it is healthy,
   - records the running version as `TAG` in `.env`.

On the **first** deploy, open `https://wellness.example.com` and **create your account straight away**. With `ALLOW_REGISTRATION=false` the first registration is allowed and every later one is refused (`403`), and the login page hides "Create account". Register right after deploying so that first account is yours.

```bash
curl -s https://wellness.example.com/api/health      # {"status":"ok"}
curl -s https://wellness.example.com/api/config      # {"registration_open":false} once you've registered
```

> The registry package is private by default. The images are labelled with this repository, which is what lets the workflow's own token pull them, so no registry credential needs to be stored on the droplet. If the very first deploy fails with `denied`, see Troubleshooting.

### Roll back

Each deploy's images stay on the droplet, tagged by commit (`docker image prune -a` would remove them and with them the quick rollback; they are small, but clear old ones occasionally if the disk fills). To go back to a previous version:

```bash
cd /opt/evergreen
sed -i 's/^TAG=.*/TAG=<older-commit-sha>/' .env
docker compose up -d --wait
```

Or re-run the workflow on an older commit. Database changes are forward-only: an older build refuses to start against a database that a newer release has already migrated, so after a schema change a rollback means restoring the pre-deploy backup (below) as well.

## 7. Backups

Data lives in the Postgres container's volume. Back it up with:

```bash
cd /opt/evergreen && ./backup.sh
# -> ./backups/evergreen-20261111-030000.dump   (compressed, mode 0600; newest 14 kept, see BACKUP_KEEP)
```

It runs `pg_dump` inside the database container, so it's safe while the app is serving traffic. A backup is also taken automatically before every deploy. Schedule a nightly one with cron (`crontab -e` as `deploy`):

```cron
0 3 * * * cd /opt/evergreen && ./backup.sh >> backup.log 2>&1
```

Backups on the same droplet don't protect you if the droplet is lost. **Copy them off the box**, for example from your own machine:

```bash
rsync -av deploy@wellness.example.com:/opt/evergreen/backups/ ./evergreen-backups/
```

…or sync them to DigitalOcean Spaces with `rclone`. If a backup leaves your control, encrypt it first (`age` or `gpg`). Droplet **backups/snapshots** are a worthwhile second layer.

### Restore

```bash
cd /opt/evergreen
./restore.sh backups/evergreen-YYYYMMDD-HHMMSS.dump          # asks you to type "restore"
```

This replaces the database with the snapshot's contents. It first checks the file is a readable backup (so a bad file is rejected before anything is touched), restores in a single transaction (so a failure leaves your current data as it was), and always brings the app back up afterwards. Both the success and failure paths were tested.

## 8. Is my data safe across deployments?

Yes, as long as the volume survives. Postgres' files live in the **`evergreen_pgdata`** Docker volume (the `evergreen_` prefix comes from `COMPOSE_PROJECT_NAME`, which the deploy pins in `.env` so it can't change if you rename the directory). On the host it's under `/var/lib/docker/volumes/evergreen_pgdata/`.

| Event | Data |
| --- | --- |
| A deploy (new image tag, containers recreated) | kept |
| `docker compose down` / `up`, a reboot, a Docker upgrade | kept |
| Rebuilding or pruning images (`docker image prune`) | kept |
| **`docker compose down -v`**, `docker volume rm`, or `docker volume prune` / `docker system prune --volumes` after the containers have been removed (`docker compose down`) | **deleted** |
| Destroying the droplet | **deleted**, unless you copied backups off it |
| Starting the stack under a different project name or directory | looks like data loss (a new, empty volume); the old one is still there. The deploy pins the name to avoid this |

> **Never run `docker compose down -v`** on the droplet unless you mean to delete the database.

To make the data survive losing the droplet itself, use a **DigitalOcean Managed Database** (automatic daily backups and point-in-time recovery) and set `DATABASE_URL=postgres://…?sslmode=require` in `.env`. The app uses whatever Postgres that URL points at. The bundled `db` container then sits idle; remove the `db` service and the API's `depends_on` from your copy of the compose file to stop running it.

## 9. Without GitHub Actions

You can deploy by hand: clone the repo on the droplet, create `.env` as in step 4, and build there:

```bash
git clone <your-repo-url> /opt/evergreen && cd /opt/evergreen
cp .env.example .env && nano .env        # set POSTGRES_PASSWORD, SITE_ADDRESS, ports, ALLOW_REGISTRATION
docker compose up -d --build
```

Building Go and the frontend needs some memory; on a 1 GB droplet add swap first. To update: `./backup.sh && git pull && docker compose up -d --build`.

## 10. Operating it

```bash
docker compose ps
docker compose logs -f              # everything
docker compose logs -f web          # Caddy: certificates, access errors
docker compose logs -f api db
docker compose restart api
docker compose down                 # stop, keep data
```

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Deploy job fails at `ssh` with "Host key verification failed" | `DROPLET_SSH_KNOWN_HOSTS` doesn't match the droplet. Re-run `ssh-keyscan -t ed25519 <host>` and update the secret. |
| Deploy job: "POSTGRES_PASSWORD is not set in .env" | Create `/opt/evergreen/.env` (step 4). |
| `docker compose pull` fails with `denied` / `unauthorized` | The images aren't linked to the repo (first push predates the label) or the package visibility was changed. Check the package's *Manage Actions access* includes this repository. |
| Browser says the connection isn't private / no certificate | DNS hasn't propagated yet, ports 80/443 are blocked (`ufw` or the Cloud Firewall), or `SITE_ADDRESS` is wrong. Check `docker compose logs web` for the ACME error. |
| API keeps restarting: "password authentication failed" | `POSTGRES_PASSWORD` in `.env` differs from the one the database was created with. The password is only applied at first creation; restore the original value (or change the database user's password with `ALTER USER`). |
| "too many failed attempts" | Sign-in is rate limited per email (5 failures) and per address (20). Wait a minute and retry. |
| Need a second user | Set `ALLOW_REGISTRATION=true`, `docker compose up -d`, register them, then set it back to `false`. |
| Let's Encrypt rate limit | Don't delete the `caddy-data` volume; it holds your certificates. |

## What's configurable

| Variable | Default | Purpose |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | `evergreen` (local only) | Database password. **Required** for deploys. |
| `SITE_ADDRESS` | `:80` | `:80` = plain HTTP (local). A domain name = automatic HTTPS. |
| `HTTP_PORT` / `HTTPS_PORT` | `8080` / `8443` | Host ports. Use `80` / `443` on the droplet. |
| `ALLOW_REGISTRATION` | `true` | `false` locks sign-ups after the first account. |
| `DATABASE_URL` | bundled Postgres | Point at any Postgres, e.g. a managed database. |
| `POSTGRES_PORT` | `5432` | Host port for the bundled database (loopback only). |
| `BACKUP_DIR` / `BACKUP_KEEP` | `./backups` / `14` | Where `backup.sh` writes and how many dumps it keeps. |
| `COOKIE_SECURE` | auto | Normally unnecessary: cookies are marked `Secure` automatically behind HTTPS. Set `true` to force it. |
