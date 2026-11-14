package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type IntegrityEntry struct {
	ID       int64
	At       string
	Jumlah   int
	Hash     string
	PrevHash string
	Oleh     string
}

// ComputeSnapshot menghitung sidik jari SHA-256 dari seluruh data pemilih
// dalam bentuk kanonik. Perubahan satu karakter pada satu baris akan
// mengubah hash.
func (s *Store) ComputeSnapshot() (string, int, error) {
	rows, err := s.DB.Query(`SELECT id, timses_id, nik, nama, dusun, rt, rw FROM voters ORDER BY id`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()

	h := sha256.New()
	n := 0
	for rows.Next() {
		var id, tid int64
		var nik, nama, dusun, rt, rw string
		if err := rows.Scan(&id, &tid, &nik, &nama, &dusun, &rt, &rw); err != nil {
			return "", 0, err
		}
		fmt.Fprintf(h, "%d|%d|%s|%s|%s|%s|%s\n", id, tid, nik, nama, dusun, rt, rw)
		n++
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (s *Store) lastIntegrityHash() (string, error) {
	var h string
	err := s.DB.QueryRow(`SELECT hash FROM integrity_log ORDER BY id DESC LIMIT 1`).Scan(&h)
	if err != nil {
		return "", nil // belum ada catatan
	}
	return h, nil
}

// RecordIntegrity mencatat hash berantai: hash baru = sha256(hash lama + hash data).
// Dengan begitu menghapus satu baris catatan di tengah akan memutus rantai.
func (s *Store) RecordIntegrity(oleh string) (*IntegrityEntry, error) {
	payload, n, err := s.ComputeSnapshot()
	if err != nil {
		return nil, err
	}
	prev, err := s.lastIntegrityHash()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(prev + "|" + payload))
	hash := hex.EncodeToString(sum[:])

	now := Now()
	res, err := s.DB.Exec(`INSERT INTO integrity_log(at,jumlah,hash,prev_hash,oleh) VALUES(?,?,?,?,?)`,
		now, n, hash, prev, oleh)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &IntegrityEntry{ID: id, At: now, Jumlah: n, Hash: hash, PrevHash: prev, Oleh: oleh}, nil
}

func (s *Store) ListIntegrity(limit int) ([]IntegrityEntry, error) {
	rows, err := s.DB.Query(`SELECT id, at, jumlah, hash, prev_hash, COALESCE(oleh,'')
		FROM integrity_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IntegrityEntry
	for rows.Next() {
		var e IntegrityEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Jumlah, &e.Hash, &e.PrevHash, &e.Oleh); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
