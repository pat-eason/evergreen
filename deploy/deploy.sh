#!/usr/bin/env bash
# Runs ON the droplet (copied there by the GitHub Actions deploy job).
#
#   echo "$GHCR_TOKEN" | ./deploy.sh <image-tag> <registry> <ghcr-user>
#
# Takes a database backup, pulls the new images, and restarts the app. Data lives
# in the pgdata Docker volume and is never touched by a deploy.
set -euo pipefail
cd "$(dirname "$0")"

if [ $# -ne 3 ]; then
  echo "usage: echo \$GHCR_TOKEN | $0 <image-tag> <registry> <ghcr-user>" >&2
  exit 2
fi
tag=$1 registry=$2 user=$3

[ -f .env ] || { echo ".env is missing. Create it from .env.example first (see DEPLOY.md)." >&2; exit 1; }
if ! grep -Eq '^POSTGRES_PASSWORD=.+' .env; then
  echo "POSTGRES_PASSWORD is not set in .env. Refusing to deploy with the default database password." >&2
  exit 1
fi

set_env() { # set_env KEY VALUE: update or append a line in .env
  if grep -q "^$1=" .env; then sed -i.bak "s|^$1=.*|$1=$2|" .env && rm -f .env.bak; else printf '%s=%s\n' "$1" "$2" >> .env; fi
}
# Pin the project name so the data volume is always <name>_pgdata, whatever this directory is called.
grep -q '^COMPOSE_PROJECT_NAME=' .env || set_env COMPOSE_PROJECT_NAME evergreen

# Back up first. If the database is up and the backup fails, stop: don't deploy over data we can't restore.
if docker compose ps --status running --services 2>/dev/null | grep -qx db; then
  echo "==> backing up"
  ./backup.sh
else
  echo "==> no running database yet (first deploy); skipping backup"
fi

echo "==> pulling $tag"
docker login ghcr.io -u "$user" --password-stdin > /dev/null
trap 'docker logout ghcr.io > /dev/null 2>&1 || true' EXIT
set_env REGISTRY "$registry"
set_env TAG "$tag"
docker compose pull
docker logout ghcr.io > /dev/null 2>&1 || true

echo "==> starting"
docker compose up -d --remove-orphans --wait
docker image prune -f > /dev/null
docker compose ps
echo "deployed $tag"
