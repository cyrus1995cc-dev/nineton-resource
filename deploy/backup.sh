#!/bin/sh
# /etc/asset-mysql-backup.cnf holds [client] user/password/socket, mode 0600.
set -eu
umask 077
destination=${ASSET_BACKUP_DIR:?Set an independent backup directory}
mkdir -p "$destination"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
temporary="$destination/asset-system-$stamp.sql.part"
trap 'rm -f -- "$temporary"' EXIT HUP INT TERM
mysqldump --defaults-extra-file=/etc/asset-mysql-backup.cnf --single-transaction --quick --no-tablespaces --set-gtid-purged=OFF --default-character-set=utf8mb4 asset_system > "$temporary"
gzip "$temporary"
mv -- "$temporary.gz" "$destination/asset-system-$stamp.sql.gz"
echo "$destination/asset-system-$stamp.sql.gz"
