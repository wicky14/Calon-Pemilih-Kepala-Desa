package store

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

var (
	ErrNotFound    = errors.New("data tidak ditemukan")
	ErrKuota       = errors.New("kuota sudah penuh")
	ErrNIK         = errors.New("NIK sudah terdaftar")
	ErrNoUrut      = errors.New("nomor urut bentrok")
	ErrUsername    = errors.New("username sudah dipakai")
	ErrTimsesNama  = errors.New("nama timses sudah dipakai")
	ErrDusunExists = errors.New("dusun sudah ada")
	ErrTerpakai    = errors.New("masih dipakai data lain")
)

type Config struct {
	NamaDesa     string
	DefaultKuota int
	Terkunci     bool
}

type Store struct {
	DB *sql.DB

	mu  sync.RWMutex
	cfg Config
}

func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Satu koneksi: SQLite hanya punya satu penulis. Ini menghilangkan
	// seluruh kemungkinan "database is locked" pada skala aplikasi ini.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("migrasi skema: %w", err)
	}

	s := &Store{DB: db}
	if err := s.seed(); err != nil {
		return nil, err
	}
	if err := s.ReloadConfig(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) seed() error {
	defaults := []struct{ k, v string }{
		{"default_kuota", "20"},
		{"nama_desa", "Desa Sukamaju"},
		{"data_terkunci", "0"},
	}
	for _, d := range defaults {
		if _, err := s.DB.Exec(`INSERT OR IGNORE INTO settings(key,value) VALUES(?,?)`, d.k, d.v); err != nil {
			return err
		}
	}
	for _, nama := range []string{"Krajan", "Tegalsari", "Sumberejo"} {
		if _, err := s.DB.Exec(`INSERT OR IGNORE INTO dusun(nama) VALUES(?)`, nama); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReloadConfig() error {
	rows, err := s.DB.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return err
	}
	defer rows.Close()

	cfg := Config{DefaultKuota: 20}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		switch k {
		case "nama_desa":
			cfg.NamaDesa = v
		case "default_kuota":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.DefaultKuota = n
			}
		case "data_terkunci":
			cfg.Terkunci = v == "1"
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return nil
}

func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return err
	}
	return s.ReloadConfig()
}

func Now() string { return time.Now().UTC().Format(time.RFC3339) }

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "KUOTA_PENUH"):
		return ErrKuota
	case strings.Contains(msg, "voters.nik"):
		return ErrNIK
	case strings.Contains(msg, "voters.timses_id, voters.no_urut"):
		return ErrNoUrut
	case strings.Contains(msg, "users.username"):
		return ErrUsername
	case strings.Contains(msg, "timses.nama"):
		return ErrTimsesNama
	case strings.Contains(msg, "dusun.nama"):
		return ErrDusunExists
	}
	return err
}
