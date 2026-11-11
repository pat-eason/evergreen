# Deploying to a DigitalOcean droplet

This guide takes a fresh droplet to a running, HTTPS-enabled copy of the app. It uses the same `docker-compose.yml` as local development; production is just a few settings in `.env`.

What you get:

- **HTTPS automatically.** Caddy obtains and renews a Let's Encrypt certificate for your domain and redirects HTTP to HTTPS.
- **Secure sessions.** Cookies are `HttpOnly`, `SameSite=Lax` and `Secure` whenever the request arrives over HTTPS.
- **Closed sign-ups.** Once your account exists, nobody else can register.
- **A real backup and restore path** for the SQLite database.

> The app stores personal financial data in a single SQLite file. Treat the droplet and its backups accordingly.

## 1. Before you start

| You need | Notes |
| --- | --- |
| A droplet | Ubuntu 24.04 LTS. The app is light (a Go binary, SQLite and Caddy), so **the smallest Basic size with 1 vCPU and 1–2 GB RAM is plenty**. Add your SSH key when creating it. |
| A domain name | Needed for HTTPS. Create an **A record** (e.g. `wellness.example.com`) pointing at the droplet's IP. Let DNS propagate before step 4. |
| The repo | Either `git clone` it on the droplet (step 4) or copy it up with `rsync`. |

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

> Docker publishes container ports by editing iptables directly, which bypasses `ufw`. That's fine here because the compose file only publishes 80 and 443; the API container is never exposed to the host.

## 3. Install Docker

```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker deploy        # then log out and back in
docker compose version                # confirm the compose plugin is present
```

## 4. Deploy

```bash
sudo mkdir -p /opt/evergreen && sudo chown deploy:deploy /opt/evergreen
git clone <your-repo-url> /opt/evergreen
cd /opt/evergreen
cp .env.example .env
nano .env
```

Set these in `.env`:

```bash
SITE_ADDRESS=wellness.example.com   # your domain
HTTP_PORT=80
HTTPS_PORT=443
ALLOW_REGISTRATION=false            # the first account can still be created; then sign-ups lock
```

Start it:

```bash
docker compose up -d --build
docker compose ps                   # both services should be Up (api: healthy)
```

Now open `https://wellness.example.com` and **create your account straight away**. With `ALLOW_REGISTRATION=false` the first registration is allowed and every later one is refused (`403`), and the login page hides "Create account". Register immediately after deploying, so that first account is yours.

Quick checks:

```bash
curl -s https://wellness.example.com/api/health      # {"status":"ok"}
curl -s https://wellness.example.com/api/config      # {"registration_open":false} once you've registered
```

## 5. Backups

The database lives in the `wellness-data` Docker volume. The app can write a consistent snapshot while it is running:

```bash
docker compose exec -T api api backup
# -> /data/backups/wellness-20261111-030000.db   (mode 0600; the newest 14 are kept, see BACKUP_KEEP)
```

Run it nightly with cron (`crontab -e` as `deploy`):

```cron
0 3 * * * cd /opt/evergreen && docker compose exec -T api api backup >> /var/log/evergreen-backup.log 2>&1
```

Backups inside the same droplet don't protect you if the droplet is lost. **Copy them off the box**, for example from your own machine:

```bash
ssh deploy@wellness.example.com 'cd /opt/evergreen && docker compose cp api:/data/backups -' > evergreen-backups.tar
tar -xf evergreen-backups.tar        # -> backups/wellness-*.db
```

…or pull them into DigitalOcean Spaces/another host with `rclone`. If a backup leaves your control, encrypt it first (`age` or `gpg`). Enabling DigitalOcean **droplet backups/snapshots** is a worthwhile second layer.

### Restore

Tested end to end: this puts the database back to exactly the snapshot's state.

```bash
cd /opt/evergreen
docker compose stop web api
docker compose run --rm --no-deps -u root --entrypoint sh api -c '
  cp /data/backups/wellness-YYYYMMDD-HHMMSS.db /data/wellness.db &&
  rm -f /data/wellness.db-wal /data/wellness.db-shm &&
  chown app:app /data/wellness.db'
docker compose up -d
```

To restore a snapshot you copied back from elsewhere, first put it in the volume with `docker compose cp ./snapshot.db api:/data/backups/` (the API container can be stopped), then run the commands above.

## 6. Updating

```bash
cd /opt/evergreen
docker compose exec -T api api backup        # always snapshot first
git pull
docker compose up -d --build
```

Database migrations run automatically when the API starts. Your data and certificates live in named volumes, so rebuilds don't touch them.

## 7. Operating it

```bash
docker compose logs -f              # everything
docker compose logs -f web          # Caddy: certificates, access errors
docker compose restart api
docker compose down                 # stop, keep data
```

> **Never run `docker compose down -v`** on the droplet unless you mean to delete the database and certificates.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Browser says the connection isn't private / no certificate | DNS hasn't propagated yet, ports 80/443 are blocked (`ufw` or the Cloud Firewall), or `SITE_ADDRESS` is wrong. Check `docker compose logs web` for the ACME error. |
| "too many failed attempts" | Sign-in is rate limited per email (5 failures) and per address (20). Wait a minute and retry. |
| Need a second user | Set `ALLOW_REGISTRATION=true`, `docker compose up -d`, register them, then set it back to `false`. |
| Let's Encrypt rate limit | Don't delete the `caddy-data` volume; it holds your certificates. |

## What's configurable

| Variable | Default | Purpose |
| --- | --- | --- |
| `SITE_ADDRESS` | `:80` | `:80` = plain HTTP (local). A domain name = automatic HTTPS. |
| `HTTP_PORT` / `HTTPS_PORT` | `8080` / `8443` | Host ports. Use `80` / `443` on the droplet. |
| `ALLOW_REGISTRATION` | `true` | `false` locks sign-ups after the first account. |
| `BACKUP_KEEP` | `14` | How many snapshots `api backup` retains. |
| `COOKIE_SECURE` | auto | Normally unnecessary: cookies are marked `Secure` automatically behind HTTPS. Set `true` to force it. |
