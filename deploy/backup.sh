#!/usr/bin/env bash
# Writes a compressed pg_dump of the database to ./backups and prunes old ones.
# Safe to run while the app is serving traffic.
#
#   ./backup.sh                     # from the directory with docker-compose.yml
#   BACKUP_DIR=/mnt/backups BACKUP_KEEP=30 ./backup.sh     # or put them in .env
#
# cron (nightly at 03:00):  0 3 * * * cd /opt/evergreen && ./backup.sh >> backup.log 2>&1
set -euo pipefail
cd "$(dirname "$0")"
[ -f docker-compose.yml ] || cd ..   # when run from the repo's deploy/ directory

# BACKUP_DIR / BACKUP_KEEP come from the environment, else from .env, else the defaults.
env_file_value() { # env_file_value KEY: last KEY=value line in .env (empty if absent)
  if [ -f .env ]; then grep -E "^$1=" .env | tail -n 1 | cut -d= -f2- || true; fi
}
dir=${BACKUP_DIR:-$(env_file_value BACKUP_DIR)}; dir=${dir:-./backups}
keep=${BACKUP_KEEP:-$(env_file_value BACKUP_KEEP)}; keep=${keep:-14}
case $keep in ''|*[!0-9]*) echo "BACKUP_KEEP must be a number, got '$keep'" >&2; exit 2;; esac
mkdir -p "$dir"
chmod 700 "$dir"

out="$dir/evergreen-$(date -u +%Y%m%d-%H%M%S).dump"
trap 'rm -f "$out.partial"' EXIT

# pg_dump runs inside the db container, using its own credentials.
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' > "$out.partial"
[ -s "$out.partial" ] || { echo "backup is empty; keeping nothing" >&2; exit 1; }
mv "$out.partial" "$out"
chmod 600 "$out"
echo "$out"

# keep only the newest $keep dumps (names sort chronologically)
files=()
while IFS= read -r f; do files+=("$f"); done < <(ls -1 "$dir"/evergreen-*.dump 2>/dev/null | sort)
excess=$(( ${#files[@]} - keep ))
for (( i = 0; i < excess; i++ )); do rm -f "${files[$i]}"; done
