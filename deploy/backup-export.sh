#!/bin/sh
# Installed root-owned; forced SSH command may only read verified backup files.
set -eu
directory=/var/backups/asset-system
operation=${1:-}
case "$operation" in
  metadata)
    latest=$(find "$directory" -maxdepth 1 -type f -name 'asset-system-????????T??????Z.sql.gz' -printf '%f\n' | LC_ALL=C sort | tail -n 1)
    test -n "$latest"
    checksum=$(sha256sum "$directory/$latest" | cut -d ' ' -f 1)
    size=$(stat -c '%s' "$directory/$latest")
    printf '{"name":"%s","sha256":"%s","size":%s}\n' "$latest" "$checksum" "$size"
    ;;
  export\ asset-system-*.sql.gz)
    name=${operation#export }
    printf '%s' "$name" | grep -Eq '^asset-system-[0-9]{8}T[0-9]{6}Z\.sql\.gz$'
    test -f "$directory/$name"
    test ! -L "$directory/$name"
    cat -- "$directory/$name"
    ;;
  *) exit 64 ;;
esac
