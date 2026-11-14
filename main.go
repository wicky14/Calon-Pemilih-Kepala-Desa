package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"datacollection/internal/auth"
	"datacollection/internal/store"
	"datacollection/internal/web"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:7777", "alamat:port untuk listen (default semua antarmuka, port 7777)")
	dbPath := flag.String("db", "data/app.db", "lokasi file database SQLite")
	demo := flag.Bool("demo", false, "isi data contoh (hanya untuk uji coba)")
	passwd := flag.String("passwd", "", "ganti password akun ini lalu keluar (tanpa menjalankan server)")
	flag.Parse()

	log.SetFlags(log.Ltime)

	if dir := filepath.Dir(*dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Fatalf("gagal membuat folder data: %v", err)
		}
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("gagal membuka database: %v", err)
	}
	defer st.Close()
	_ = os.Chmod(*dbPath, 0o600)

	// Mode pemulihan: ganti password satu akun lalu keluar.
	if *passwd != "" {
		if err := changePasswordCLI(st, *passwd); err != nil {
			log.Fatalf("ganti password: %v", err)
		}
		return
	}

	if n, err := st.CountUsers(); err == nil && n == 0 {
		pass := auth.RandomPassword()
		hash, err := auth.HashPassword(pass)
		if err != nil {
			log.Fatalf("gagal membuat akun admin: %v", err)
		}
		if _, err := st.CreateUser("admin", hash, "admin", nil, true); err != nil {
			log.Fatalf("gagal membuat akun admin: %v", err)
		}
		fmt.Printf(`
================================================================
  AKUN ADMIN DIBUAT
  username : admin
  password : %s
  Ganti password ini segera setelah login pertama.
================================================================
`, pass)
	}

	if *demo {
		seedDemo(st)
	}

	srv, err := web.New(st)
	if err != nil {
		log.Fatalf("gagal menyiapkan halaman: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	go func() {
		log.Printf("Aplikasi berjalan di http://%s", *addr)
		for _, u := range lanURLs(*addr) {
			log.Printf("  dari HP: %s", u)
		}
		log.Printf("Database: %s", *dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server berhenti: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Print("Menghentikan server…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// lanURLs menampilkan alamat yang bisa dibuka dari HP di WiFi yang sama.
func lanURLs(addr string) []string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil {
				continue
			}
			out = append(out, "http://"+ip.String()+":"+port)
		}
	}
	return out
}

func seedDemo(st *store.Store) {
	if n, _ := st.CountUsers(); n > 1 {
		log.Print("demo: data contoh dilewati (sudah ada akun lain)")
		return
	}
	list, err := st.ListTimses()
	if err != nil || len(list) > 0 {
		return
	}

	type seed struct {
		nama, user string
	}
	teams := []seed{
		{"Timses 01 — RW 02", "tim01"},
		{"Timses 02 — RW 03", "tim02"},
	}
	names := []string{
		"Budi Santoso", "Siti Aminah", "Ahmad Fauzi", "Dewi Lestari",
		"Rudi Hartono", "Nur Aisyah", "Joko Prasetyo", "Wati Sumarni",
		"Eko Purwanto", "Rina Marlina", "Agus Setiawan", "Sri Wahyuni",
	}
	dusun := []string{"Krajan", "Tegalsari", "Sumberejo"}

	pass := "timses123"
	hash, err := auth.HashPassword(pass)
	if err != nil {
		return
	}

	for ti, t := range teams {
		id, err := st.CreateTimses(t.nama, nil)
		if err != nil {
			log.Printf("demo: %v", err)
			continue
		}
		tid := id
		if _, err := st.CreateUser(t.user, hash, "timses", &tid, false); err != nil {
			log.Printf("demo: %v", err)
		}
		for i := 0; i < 8; i++ {
			idx := ti*8 + i
			in := store.VoterInput{
				Nama:  names[idx%len(names)],
				NIK:   fmt.Sprintf("3507%02d%02d%02d%02d%04d", ti+1, i+1, (i*7)%12+1, i%28+1, idx+1),
				Dusun: dusun[idx%len(dusun)],
				RT:    fmt.Sprintf("%03d", i%5+1),
				RW:    fmt.Sprintf("%03d", ti+2),
			}
			if _, err := st.CreateVoter(id, in, 1, t.user); err != nil {
				log.Printf("demo: %v", err)
			}
		}
	}
	log.Printf("demo: %d timses dibuat, password semua akun: %s", len(teams), pass)
}
