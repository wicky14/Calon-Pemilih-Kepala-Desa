#!/usr/bin/env bash
# Backup database SQLite tanpa menghentikan aplikasi.
# Pemakaian:
#   ./deploy/backup.sh
#   BACKUP_PASSPHRASE='sandi-panjang' ./deploy/backup.sh   # terenkripsi GPG
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DB="${DB:-$APP_DIR/data/app.db}"
DEST="${DEST:-$APP_DIR/backup}"
KEEP_DAYS="${KEEP_DAYS:-14}"

if [ ! -f "$DB" ]; then
  echo "Database tidak ditemukan: $DB" >&2
  exit 1
fi
if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "Butuh sqlite3 CLI. Pasang: sudo pacman -S sqlite" >&2
  exit 1
fi

mkdir -p "$DEST"
chmod 700 "$DEST"

stamp="$(date +%Y%m%d-%H%M%S)"
out="$DEST/app-$stamp.db"

umask 077
sqlite3 "$DB" ".backup '$out'"
chmod 600 "$out"

if [ -n "${BACKUP_PASSPHRASE:-}" ] && command -v gpg >/dev/null 2>&1; then
  gpg --batch --yes --quiet --symmetric --cipher-algo AES256 \
      --passphrase "$BACKUP_PASSPHRASE" "$out"
  rm -f "$out"
  echo "Backup terenkripsi: $out.gpg"
else
  echo "Backup dibuat: $out"
fi

find "$DEST" -type f \( -name 'app-*.db' -o -name 'app-*.db.gpg' \) \
     -mtime "+$KEEP_DAYS" -delete
echo "Selesai. Menyimpan berkas $KEEP_DAYS hari terakhir."
