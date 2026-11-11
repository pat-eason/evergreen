#!/usr/bin/env bash
# Replaces the database with the contents of a dump made by backup.sh.
#
#   ./restore.sh backups/evergreen-20261111-030000.dump        # asks for confirmation
#   ./restore.sh backups/evergreen-20261111-030000.dump --yes   # no prompt
#
# The restore is a single transaction: if anything fails, the current data is left untouched
# and the app is restarted.
set -euo pipefail
cd "$(dirname "$0")"
[ -f docker-compose.yml ] || cd ..

dump=${1:-}
if [ -z "$dump" ] || [ ! -f "$dump" ]; then
  echo "usage: $0 <backup.dump> [--yes]" >&2
  exit 2
fi
if [ "${2:-}" != "--yes" ]; then
  echo "This REPLACES everything in the database with $dump."
  read -r -p "Type 'restore' to continue: " answer
  [ "$answer" = "restore" ] || { echo "aborted"; exit 1; }
fi

# Reject an unreadable file before taking the app down.
docker compose up -d --wait db
if ! docker compose exec -T db pg_restore --list < "$dump" > /dev/null 2>&1; then
  echo "$dump is not a valid backup made by backup.sh; nothing was changed." >&2
  exit 1
fi

# From here on, however this script ends, the app must come back up.
ok=0
trap '[ "$ok" = 1 ] || { echo "Restore FAILED. The restore is all-or-nothing, so your existing data was not changed. Restarting the app." >&2; docker compose up -d; }' EXIT

docker compose stop web api
docker compose exec -T db sh -c 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --single-transaction --clean --if-exists --no-owner' < "$dump"
ok=1
docker compose up -d --wait
echo "restored from $dump"
