# Deploying to the shared DigitalOcean droplet

Evergreen runs on a droplet it shares with other apps, behind one shared Caddy reverse proxy, at
**`https://apps.pateason.dev/evergreen/`** (a path prefix on a shared hostname, not its own domain). The server-level
pieces (Docker, the `deploy` user, the firewall, the proxy and its certificate) live in the **droplet-infra** repo;
this repo only deploys its own containers and registers its own route with that proxy. Read the "contract every app
follows" and "Path-based routing" sections of that README first.

Every push to `main` runs `.github/workflows/deploy.yml`:

1. **test**: gofmt check, `go vet`, `go build`, `go test -race` against a real Postgres service, then the frontend
   typecheck and build. Pull requests run only this job.
2. **images**: builds `ghcr.io/pat-eason/evergreen-api` and `evergreen-web`, tagged with the commit SHA. The web image
   is built with `BASE_PATH=/evergreen/` (see "Serving under a path").
3. **deploy**: copies the compose files to `/opt/evergreen`, backs up the database (if it is already running), pulls the
   images, runs `docker compose up -d --no-build`, waits for the API health check, and finally runs
   `proxy-site install evergreen` with `deploy/evergreen.caddy`.

> The app stores personal financial data. Treat the droplet, its `.env`, and every backup accordingly.

## How it plugs into the proxy

`docker-compose.prod.yml` is layered over `docker-compose.yml` on the droplet (`COMPOSE_FILE` is set in `.env`):

| Contract rule | How Evergreen meets it |
| --- | --- |
| External network `proxy` | the overlay declares it `external: true` (created by droplet-infra's setup script) |
| Unique alias | the `web` service joins `proxy` as **`evergreen-web`** (and keeps its private default network) |
| No published ports | `web` and `db` use `ports: !reset []`; `api` never published any |
| Plain HTTP on port 80 | `SITE_ADDRESS=:80`: the web container's own Caddy serves HTTP and gets no certificate; the shared proxy does HTTPS |

The site snippet, `deploy/evergreen.caddy`, routes `/evergreen/*` to `evergreen-web:80` and strips the prefix, so the
web container always sees requests at `/`. `proxy-site` validates the whole Caddy config and rolls back if the snippet
is invalid.

**Never** join `db` or `api` to the `proxy` network, and never add `ports:` to them. Only the web container is reachable
from other apps; the database and API stay on the app's private network.

### Serving under a path

All apps share one origin, so Evergreen is path-aware:

- **Assets and routes:** the frontend is built with `BASE_PATH=/evergreen/` (Docker build arg; default `/` for local
  use). Vite prefixes asset URLs, the router uses the same base, and API calls go to `<base>api/...`.
- **Cookie:** the session cookie is `evergreen_session` with `Path=/evergreen` (`COOKIE_PATH`), `HttpOnly`,
  `SameSite=Lax` and `Secure`. The overlay sets `COOKIE_SECURE=true`, because the app only ever sees plain HTTP from
  the proxy (the web container's Caddy also rewrites `X-Forwarded-Proto`).
- **Browser storage:** localStorage keys are prefixed `evergreen.`.
- **Client IP:** the web container trusts `X-Forwarded-For` from private addresses only (`trusted_proxies` in the
  Caddyfile), so the login rate limit sees real client addresses rather than the shared proxy's.
- The API has no redirects and generates no absolute URLs.

## Security: sharing a droplet

Apps on one droplet share a kernel, disk and Docker daemon. The `deploy` user is in the `docker` group, which is
root-equivalent, so **a compromise of any app or any deploy key is a compromise of every app on the box**, including
this one's financial data. The private network and separate databases limit the blast radius but don't remove it.
Mitigations: one deploy key per repo, `ALLOW_REGISTRATION=false`, off-droplet backups, and, if the data is sensitive
enough, a separate droplet. Registration is closed by default: the first `.env` sets `ALLOW_REGISTRATION=false`, which
still lets the first account be created and refuses every later one. **Create your account right after the first deploy.**

## One-time setup

1. **Droplet and proxy:** set up the droplet and the shared proxy from droplet-infra (its README walks through it).
   The proxy must be running and the `proxy` network must exist.
2. **App folder:** `sudo install -d -o deploy -g deploy /opt/evergreen`.
3. **Deploy key (per repo):** `ssh-keygen -t ed25519 -C evergreen-deploy -f deploy_key -N ""`, append `deploy_key.pub`
   to `/home/deploy/.ssh/authorized_keys` on the droplet, then delete the local key files after saving the secret.
4. **GitHub secrets** (Settings, Secrets and variables, Actions):

   | Kind | Name | Value |
   | --- | --- | --- |
   | Secret | `DROPLET_HOST` | droplet IP or hostname |
   | Secret | `DROPLET_USER` | `deploy` |
   | Secret | `DROPLET_SSH_KEY` | the private key from step 3 |
   | Secret | `DROPLET_KNOWN_HOSTS` | output of `ssh-keyscan -t ed25519 <droplet-ip>` (verify the fingerprint) |
   | Secret | `DROPLET_SSH_PORT` | optional, default 22 |

   No domain variable is needed: the hostname belongs to the proxy.
5. **`production` environment (recommended):** Settings, Environments, New environment. Add yourself as a required
   reviewer to gate each deploy.
6. Push to `main` (or Actions, *CI & Deploy*, Run workflow). The first deploy creates `/opt/evergreen/.env` with a
   random `POSTGRES_PASSWORD`. It is **never overwritten afterwards**, because Postgres reads that password only when
   its volume is first created. Keep a copy in a password manager.

Check it: `curl -s https://apps.pateason.dev/evergreen/api/health` returns `{"status":"ok"}`.

The registry package is private by default. The images are labelled with this repository, which lets the workflow's own
token pull them; no registry credential is stored on the droplet (the deploy logs in, pulls, and logs out).

## Backups

Postgres lives in the `evergreen_pgdata` volume. Deploys, `down`, reboots and image pruning keep it; `docker compose
down -v`, `docker volume rm` or destroying the droplet delete it. **Never run `down -v`** unless you mean to wipe the data.

```bash
cd /opt/evergreen
./backup.sh        # -> ./backups/evergreen-YYYYMMDD-HHMMSS.dump (pg_dump custom format, mode 0600, newest 14 kept)
```

It runs `pg_dump` inside the database container, so it is safe while the app is serving. The deploy also runs it before
every release. Nightly via cron (`crontab -e` as `deploy`):

```cron
0 3 * * * cd /opt/evergreen && ./backup.sh >> backup.log 2>&1
```

Backups on the droplet do not survive losing the droplet. **Copy them off it**, e.g. from your own machine:

```bash
rsync -av deploy@<droplet>:/opt/evergreen/backups/ ./evergreen-backups/
```

Encrypt dumps (`age`, `gpg`) before they leave your control, and consider DigitalOcean droplet backups as a second layer.

### Restore

```bash
cd /opt/evergreen
./restore.sh backups/evergreen-YYYYMMDD-HHMMSS.dump      # asks you to type "restore"
```

It checks the file is a readable dump first, restores in a single transaction (a failure leaves current data intact),
and always brings the app back up. Copy a dump from your machine back with `scp` first if the droplet was rebuilt.

## Roll back

Images are tagged by commit SHA. To roll back, **re-run the workflow run of the older commit** (Actions, pick the run,
Re-run all jobs); it redeploys those exact images. Database changes are forward-only: an older build refuses to start
against a database a newer release already migrated, so after a schema change a rollback also means restoring the
pre-deploy backup (above).

## Operating it

```bash
cd /opt/evergreen
docker compose ps
docker compose logs -f api db web
docker compose restart api
docker network inspect proxy --format '{{range .Containers}}{{.Name}} {{end}}'   # who is on the proxy network
proxy-site list
```

Need a second user? Set `ALLOW_REGISTRATION=true` in `.env`, `docker compose up -d`, register them, then set it back.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| `Host key verification failed` | `DROPLET_KNOWN_HOSTS` doesn't match the droplet; re-run `ssh-keyscan -t ed25519 <host>`. |
| `scp` fails with "No such file or directory" | `/opt/evergreen` doesn't exist yet (setup step 2). |
| `network proxy declared as external, but could not be found` | droplet-infra's setup hasn't run (or `docker network create proxy`). |
| `docker compose pull` says `denied` | the package isn't linked to the repo; check the package's *Manage Actions access* includes this repository. |
| `/evergreen/` returns 502 | the web container isn't on `proxy`, or its alias isn't `evergreen-web`. Check the network inspect above. |
| Blank page, assets 404 | the web image was built without `BASE_PATH=/evergreen/` (a local `docker compose build` produces a root-path image). |
| Logged out immediately after sign-in | cookie not sent: the page isn't being served under `/evergreen/`, or the connection is not HTTPS (the cookie is `Secure`). |
| API restarts: "password authentication failed" | `POSTGRES_PASSWORD` in `.env` differs from the one the database was created with (applied only at first creation). |
| "too many failed attempts" | sign-in is rate limited per email (5) and per address (20); wait a minute. |

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | `evergreen` (local only) | Database password; generated on the first deploy. |
| `ALLOW_REGISTRATION` | `true` (`false` in the droplet `.env`) | `false` locks sign-ups after the first account. |
| `COOKIE_SECURE` | `true` in production | Force the `Secure` flag on the session cookie. |
| `COOKIE_PATH` | `/` (`/evergreen` in production) | Session cookie path. |
| `BASE_PATH` | `/` | Build arg: public path of the SPA (CI uses `/evergreen/`). |
| `DATABASE_URL` | bundled Postgres | Point at any Postgres, e.g. a managed database. |
| `BACKUP_DIR` / `BACKUP_KEEP` | `./backups` / `14` | Where `backup.sh` writes and how many it keeps. |
| `REGISTRY` / `TAG` | `ghcr.io/pat-eason` / `latest` | Image location and version; the deploy records these in `.env`. |
| `COMPOSE_PROJECT_NAME` / `COMPOSE_FILE` | set in `.env` | Pin the volume name and layer the prod overlay. |
