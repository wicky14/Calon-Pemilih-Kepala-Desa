# Aplikasi Pendataan Calon Pemilih Kepala Desa

Aplikasi web sederhana untuk mengumpulkan data calon pemilih per timses.
Satu binary Go, satu file SQLite, tanpa framework, tanpa `node_modules`,
tanpa server database. Dirancang untuk dipakai di **jaringan lokal (LAN/WiFi)**
dari HP masing-masing petugas.

> Panduan menjalankan, memasang sebagai layanan, dan backup ada di
> **[CARA-MENJALANKAN.md](CARA-MENJALANKAN.md)**.

## Fitur

- Login admin dan timses, akun dibuat oleh admin (tanpa pendaftaran mandiri).
- Setiap timses hanya bisa melihat dan mengubah **datanya sendiri**.
- Batas kuota per timses (default 20, bisa diatur per timses).
- **NIK unik** dijamin oleh database, bukan hanya oleh tampilan.
- Kuota dijaga trigger database, aman walau banyak orang menyimpan bersamaan.
- Nomor urut otomatis per timses.
- Export CSV: `Nomor, Nama, NIK, Dusun, No. RT, No. RW`.
- Pencarian nama/NIK.
- Kunci data di akhir periode.
- Riwayat perubahan (audit log) dan sidik jari data (hash berantai).
- Tampilan mobile-first, mode gelap otomatis, siap "Tambahkan ke Layar Utama".

## Kebutuhan

- Linux x86_64 (atau build sendiri untuk arsitektur lain)
- Go 1.24+ **hanya untuk membangun**; menjalankan tidak butuh Go
- `sqlite3` CLI hanya untuk skrip backup (opsional)

## Mulai cepat

```bash
go build -o data-collection .
./data-collection
```

Buka `http://localhost:7777`. Server mendengarkan di **semua antarmuka, port 7777**.

Saat pertama kali dijalankan, aplikasi membuat akun admin dan menampilkan
password-nya **sekali** di terminal:

```
================================================================
  AKUN ADMIN DIBUAT
  username : admin
  password : k7F2-mQ9x-Tb4p
  Ganti password ini segera setelah login pertama.
================================================================
```

Setelah login, admin wajib mengganti password tersebut.

### Opsi

| Flag | Default | Keterangan |
|---|---|---|
| `-addr` | `0.0.0.0:7777` | alamat dan port |
| `-db` | `data/app.db` | lokasi file database |
| `-demo` | `false` | isi data contoh (2 timses, password `timses123`) |

Contoh untuk uji coba tanpa mengotori data asli:

```bash
./data-collection -demo -db /tmp/coba.db -addr 127.0.0.1:7777
```

## Cara pakai (alur kerja)

1. **Admin** login → menu **Pengaturan**: isi nama desa, kuota default, daftar dusun.
2. Menu **Timses** → tambah timses. Satu form langsung membuat akun login
   (`tim01`, `tim02`, …) beserta password. Catat password yang muncul.
3. Bagikan alamat `http://<IP-laptop>:7777`, username, dan password ke koordinator timses.
4. **Timses** login dari HP → tambah data pemilih. Nomor urut otomatis.
5. Admin memantau progres di **Dashboard** dan bisa export kapan saja.
6. Setelah selesai: **Pengaturan → Kunci semua data**, lalu export final.

## Keamanan

Yang sudah diterapkan:

- Password: PBKDF2-SHA256 600.000 iterasi, salt acak per akun.
- Sesi: token 32 byte acak, disimpan sebagai SHA-256 di database,
  cookie `HttpOnly` + `SameSite=Lax`, berlaku 12 jam.
- Rate limit login: 5 percobaan gagal per 5 menit per IP+username.
- CSRF token pada setiap form.
- Semua query memakai prepared statement; template auto-escape (anti-XSS).
- Isolasi data per timses dicek di server pada setiap baca/tulis/hapus.
- NIK ganda ditolak di level database (`UNIQUE`).
- Audit log dan sidik jari SHA-256 berantai untuk deteksi perubahan.
- Menonaktifkan timses/akun langsung memutus sesinya.

**Wajib diperhatikan:**

1. **Hanya untuk LAN.** Jangan buka port ini ke internet tanpa HTTPS.
   Untuk LAN rumah/posko gunakan WiFi berpassword (WPA2/WPA3).
2. **Backup harian.** Laptop rusak/hilang adalah risiko terbesar.
   Jalankan `deploy/backup.sh` lewat timer; simpan hasilnya di perangkat lain.
3. **Jaga laptop server.** File `data/app.db` memuat NIK asli (plaintext).
   Izin file diatur `0600`, tetapi akses fisik tetap harus dibatasi.
4. Ganti password admin, dan nonaktifkan akun timses yang sudah selesai.

## Pasang sebagai layanan (systemd, user)

```bash
mkdir -p ~/.config/systemd/user
cp deploy/data-collection.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now data-collection
systemctl --user status data-collection
loginctl enable-linger "$USER"   # agar tetap jalan tanpa login
```

Sesuaikan `WorkingDirectory`/`ExecStart` bila folder proyek bukan
`~/Projects/webapp/data-collection`.

## Backup harian

`deploy/backup.sh` memakai `sqlite3 .backup` (aman walau aplikasi sedang jalan),
menyimpan 14 file terakhir, dan mengenkripsi dengan GPG bila `BACKUP_PASSPHRASE` diisi.

```bash
chmod +x deploy/backup.sh
BACKUP_PASSPHRASE='kata-sandi-panjang' ./deploy/backup.sh
```

Timer harian (jam 21:00):

```bash
mkdir -p ~/.config/systemd/user
cp deploy/data-collection-backup.* ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now data-collection-backup.timer
```

Simpan folder `backup/` di drive/flashdisk lain, bukan hanya di laptop yang sama.

## Akses dari HP

1. Pastikan laptop dan HP di WiFi yang sama.
2. Cari IP laptop: `ip -4 addr show | grep inet`.
3. Buka `http://<IP>:7777` di browser HP.
4. Agar alamat tidak berubah, tetapkan IP statis di router.
5. Atur laptop agar **tidak tidur** saat layar ditutup
   (mis. `sudo systemctl mask sleep.target suspend.target hibernate.target`).
6. Di Chrome/Android: menu → "Tambahkan ke Layar Utama" agar tampil seperti aplikasi.

## Struktur

```
main.go                     titik masuk, flag, seed admin
internal/store/             skema SQL, query, integritas
internal/auth/              PBKDF2, token, rate limiter
internal/web/               routing, handler, template, CSS
deploy/                     systemd unit + skrip backup
data/app.db                 database (dibuat otomatis)
```
