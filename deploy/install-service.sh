#!/usr/bin/env bash
# Memasang aplikasi sebagai layanan systemd user, mengaktifkannya saat login,
# dan langsung menjalankannya. Aman dijalankan berulang kali.
#
#   ./deploy/install-service.sh
#
# Opsi lewat variabel lingkungan:
#   ADDR=0.0.0.0:7777   alamat listen
#   DB=/path/app.db     lokasi database
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$DIR/data-collection"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UNIT="$UNIT_DIR/data-collection.service"
ADDR="${ADDR:-0.0.0.0:7777}"
DB="${DB:-$DIR/data/app.db}"

export PATH="$HOME/.local/go/bin:$PATH"

say() { printf '%s\n' "$*"; }

# 1. Pastikan binary ada.
if [ ! -x "$BIN" ]; then
	if command -v go >/dev/null 2>&1; then
		say "Binary belum ada — membangun dengan go build…"
		(cd "$DIR" && go build -o data-collection .)
	else
		say "Binary $BIN tidak ditemukan dan Go tidak tersedia."
		say "Bangun dulu dengan: cd \"$DIR\" && go build -o data-collection ."
		exit 1
	fi
fi

# 2. Hentikan proses manual (bukan dari systemd) agar port tidak bentrok.
stop_manual() {
	local pid exe
	for pid in $(pgrep -x data-collection 2>/dev/null || true); do
		exe="$(readlink -f "/proc/$pid/exe" 2>/dev/null || true)"
		if [ "$exe" = "$BIN" ]; then
			say "Menghentikan proses manual (PID $pid)…"
			kill "$pid" 2>/dev/null || true
		fi
	done
	for _ in 1 2 3 4 5 6 7 8 9 10; do
		pgrep -x data-collection >/dev/null 2>&1 || return 0
		sleep 0.4
	done
}

# 3. Tulis unit dengan path yang benar (mengikuti lokasi proyek ini).
install_unit() {
	mkdir -p "$UNIT_DIR"
	cat >"$UNIT" <<EOF
[Unit]
Description=Pendataan Pemilih Kepala Desa
Documentation=file://$DIR/README.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$DIR
ExecStart=$BIN -addr $ADDR -db $DB
Restart=always
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=default.target
EOF
	say "Unit ditulis: $UNIT"
}

# 4. Aktifkan.
if systemctl --user is-active --quiet data-collection 2>/dev/null; then
	install_unit
	systemctl --user daemon-reload
	systemctl --user restart data-collection
else
	stop_manual
	install_unit
	systemctl --user daemon-reload
	systemctl --user enable --now data-collection
fi

# 5. Tetap hidup walau sesi login ditutup (tidak wajib berhasil).
if ! loginctl enable-linger "$USER" 2>/dev/null; then
	say "Catatan: enable-linger gagal — layanan tetap jalan selama Anda login."
fi

# 6. Tunggu aplikasi siap, lalu tampilkan ringkasan.
for _ in $(seq 1 25); do
	if curl -fsS -o /dev/null "http://127.0.0.1:${ADDR##*:}/login" 2>/dev/null; then
		break
	fi
	sleep 0.4
done

port="${ADDR##*:}"
say ""
say "Layanan aktif."
say "  Status : systemctl --user status data-collection"
say "  Restart: systemctl --user restart data-collection"
say "  Hentikan: systemctl --user stop data-collection"
say "  Log    : journalctl --user -u data-collection -f"
say ""
say "Buka: http://localhost:$port"
ip -4 -o addr show scope global 2>/dev/null | awk -v p="$port" '{split($4,a,"/"); print "  dari HP: http://" a[1] ":" p}'
say ""
systemctl --user --no-pager --lines=5 status data-collection || true
