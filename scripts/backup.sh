#!/bin/sh
# Nightly backup: database dump + screenshots, copied off the machine with rclone.
# Cron on the VPS: 30 3 * * * /path/to/fix-my-deshboard/scripts/backup.sh >> /var/log/fix-my-deshboard-backup.log 2>&1
set -eu
cd "$(dirname "$0")/.."
. ./.env

dir="backups/$(date +%F)"
mkdir -p "$dir"
docker compose exec -T db pg_dump -U fixmydeshboard fixmydeshboard | gzip > "$dir/db.sql.gz"
docker compose cp app:/data/uploads "$dir/uploads"

if [ -n "${BACKUP_REMOTE:-}" ]; then
  rclone copy "$dir" "$BACKUP_REMOTE/$(date +%F)"
fi
find backups -mindepth 1 -maxdepth 1 -mtime +7 -exec rm -rf {} +
