# Cara Menjalankan Aplikasi Pendataan Pemilih

Panduan singkat operasional. Untuk gambaran fitur dan keputusan desain,
lihat `README.md`.

Lokasi proyek: `~/Projects/webapp/data-collection`
Port aplikasi: **7777**

---

## 1. Menjalankan (cara tercepat)

```bash
cd ~/Projects/webapp/data-collection
./data-collection
```

Output di terminal:

```
================================================================
  AKUN ADMIN DIBUAT
  username : admin
  password : xxxx-xxxx-xxxx
  Ganti password ini segera setelah login pertama.
================================================================
16:59:59 Aplikasi berjalan di http://0.0.0.0:7777
16:59:59   dari HP: http://192.168.x.x:7777
```

Password admin hanya dicetak **sekali**, saat database masih kosong.
Catat, lalu ganti setelah login.

Buka di browser laptop: **http://localhost:7777**

### Kalau database sudah ada

Admin tidak dibuat ulang dan password tidak dicetak lagi. Login memakai
akun yang sudah ada. Bila password admin hilang dan tidak ada admin lain:

```bash
# BACKUP dulu, lalu mulai dari database kosong (data lama tidak terbaca lagi)
cp data/app.db data/app.db.bak
rm -f data/app.db data/app.db-wal data/app.db-shm
./data-collection
```

### Mode uji coba (data contoh)

Tidak menyentuh data asli:

```bash
./data-collection -demo -db /tmp/coba.db -addr 127.0.0.1:7777
```

Akun contoh: `tim01` / `timses123`, `tim02` / `timses123`.
Hapus `/tmp/coba.db` setelah selesai.

---

## 2. Opsi yang tersedia

| Flag | Default | Keterangan |
|---|---|---|
| `-addr` | `0.0.0.0:7777` | alamat:port. Pakai `127.0.0.1:7777` bila hanya untuk laptop sendiri |
| `-db` | `data/app.db` | lokasi file database |
| `-demo` | `false` | isi data contoh |
| `-passwd` | — | ganti password satu akun lalu keluar (lihat bagian 8) |

Contoh:

```bash
./data-collection -db /home/omakid/data-desa/app.db -addr 0.0.0.0:7777
```

Hentikan aplikasi: **Ctrl+C** (atau `kill` PID-nya).

---

## 3. Membangun ulang setelah ada perubahan kode

```bash
cd ~/Projects/webapp/data-collection
export PATH="$HOME/.local/go/bin:$PATH"   # Go dipasang di ~/.local/go
go build -o data-collection .
```

Membangun ulang **tidak menyentuh** `data/app.db`.

Alternatif memakai Makefile:

```bash
make build
make run
make demo
```

---

## 4. Akses dari HP (WiFi yang sama)

1. Cek IP laptop:

   ```bash
   ip -4 addr show | grep inet
   ```

   Contoh hasil: `inet 192.168.18.9/24` → alamatnya `http://192.168.18.9:7777`

2. Pastikan HP dan laptop di **SSID yang sama**.
3. Buka alamat itu di browser HP.
4. Opsional: Chrome/Android → menu → **Tambahkan ke Layar Utama**
   agar tampil seperti aplikasi (tanpa address bar).
5. Tetapkan IP statis di router supaya alamat tidak berubah.

### Laptop tidak boleh tidur

Kalau layar ditutup dan laptop suspend, aplikasi tidak bisa diakses.

```bash
# cegah suspend saat layar ditutup (sesuaikan dengan desktop yang dipakai)
sudo systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target
```

Kembalikan bila perlu:

```bash
sudo systemctl unmask sleep.target suspend.target hibernate.target hybrid-sleep.target
```

---

## 5. Jalankan otomatis saat boot (systemd user)

```bash
mkdir -p ~/.config/systemd/user
cp ~/Projects/webapp/data-collection/deploy/data-collection.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now data-collection
loginctl enable-linger "$USER"     # tetap jalan walau belum login
```

Perintah sehari-hari:

```bash
systemctl --user status data-collection     # cek status
systemctl --user restart data-collection    # restart
systemctl --user stop data-collection       # hentikan
journalctl --user -u data-collection -f     # lihat log langsung
```

> Bila folder proyek bukan `~/Projects/webapp/data-collection`, sesuaikan
> `WorkingDirectory` dan `ExecStart` di file service.

---

## 6. Backup harian (wajib)

Manual:

```bash
cd ~/Projects/webapp/data-collection
./deploy/backup.sh
```

Hasil di folder `backup/`, format `app-YYYYMMDD-HHMMSS.db`, menyimpan
14 hari terakhir. Bisa dijalankan **saat aplikasi hidup**.

Dengan enkripsi GPG:

```bash
BACKUP_PASSPHRASE='sandi-panjang-yang-kuat' ./deploy/backup.sh
```

Otomatis tiap hari jam 21:00:

```bash
mkdir -p ~/.config/systemd/user
cp deploy/data-collection-backup.* ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now data-collection-backup.timer
systemctl --user list-timers | grep backup
```

**Salin folder `backup/` ke flashdisk atau drive lain.** Backup di laptop
yang sama tidak melindungi dari laptop rusak/hilang.

### Memulihkan dari backup

```bash
systemctl --user stop data-collection        # bila pakai service
cd ~/Projects/webapp/data-collection
cp backup/app-20261114-210000.db data/app.db
rm -f data/app.db-wal data/app.db-shm
./data-collection
```

Untuk file `.gpg`:

```bash
gpg --decrypt backup/app-20261114-210000.db.gpg > data/app.db
```

---

## 7. Urutan pemakaian

1. Login admin → ganti password.
2. **Pengaturan**: nama desa, kuota default (20), daftar dusun.
3. **Timses**: tambah timses. Satu form membuat timses + akun + password
   (password tampil sekali — catat dan berikan ke koordinator).
4. Timses login dari HP → tambah data pemilih (Nomor otomatis).
5. Admin pantau **Dashboard**; export kapan saja lewat tombol **CSV**.
6. Setelah pendataan selesai: **Pengaturan → Kunci semua data**.
7. **Integritas → Catat sekarang**, simpan/catat hash-nya.
8. Export final, simpan CSV + backup terenkripsi di tempat aman.

---

## 8. Kalau ada masalah

**Lupa password / ingin mengganti password sendiri (tanpa login)**

Tersedia dari terminal, langsung ke database. Aplikasi **boleh tetap
berjalan**.

```bash
cd ~/Projects/webapp/data-collection
./data-collection -passwd admin
```

- Ketik password baru (minimal 8 karakter). Ketikan **tidak tampil** di layar.
- Diminta mengetik ulang untuk memastikan tidak salah.
- Semua sesi login akun itu dicabut, jadi HP yang masih login harus masuk lagi.
- Akun tidak dipaksa ganti password lagi setelah ini.
- Bisa untuk akun timses juga: `./data-collection -passwd tim01`.
- Perintah ini **hanya mengganti password**; server tidak dijalankan.

Bila tidak ingin mengetik interaktif (mis. lewat skrip):

```bash
printf 'passwordpanjang\n' | ./data-collection -passwd admin
```

Catatan: setelah mengganti password, jalankan lagi `./data-collection`
bila tadi aplikasi dihentikan.

**Port 7777 sudah dipakai**

```bash
ss -ltnp | grep 7777
# hentikan prosesnya, atau jalankan di port lain:
./data-collection -addr 0.0.0.0:7778
```

**HP tidak bisa membuka halaman**

- Pastikan satu WiFi, bukan data seluler.
- Cek IP laptop masih sama (`ip -4 addr`).
- Cek firewall:

  ```bash
  sudo ufw status
  # atau
  sudo firewall-cmd --list-all
  ```

- Coba dari laptop sendiri dulu: `curl -I http://127.0.0.1:7777/login`

**Login bilang "Terlalu banyak percobaan"**

Tunggu 5 menit, atau restart aplikasi untuk mereset penghitung.

**"Kuota timses ini sudah penuh"**

Admin → **Timses** → ubah kolom Kuota (kosongkan = ikut default).

**"NIK sudah terdaftar"**

NIK itu sudah ada di timses mana pun. Cari di **Data** (admin) untuk
menemukan pemiliknya.

**"Data sudah dikunci"**

Admin → **Pengaturan → Buka kunci data**.

**Database terkunci / aplikasi tidak mau start**

Pastikan tidak ada dua proses berjalan:

```bash
pgrep -af data-collection
```

Satu database hanya untuk satu proses.

---

## 9. Perintah ringkas

```bash
cd ~/Projects/webapp/data-collection

./data-collection                 # jalankan (port 7777)
./data-collection -demo           # uji coba dengan data contoh
./data-collection -passwd admin   # ganti password akun (tanpa login)
make build                        # build ulang
./deploy/backup.sh                # backup manual
pgrep -af data-collection         # cek proses
kill <PID>                        # hentikan
curl -I http://127.0.0.1:7777/login   # cek aplikasi hidup
```
