package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

/* ---------------------------------------------------------------- settings */

type Dusun struct {
	ID   int64
	Nama string
}

func (s *Store) ListDusun() ([]Dusun, error) {
	rows, err := s.DB.Query(`SELECT id, nama FROM dusun ORDER BY nama`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dusun
	for rows.Next() {
		var d Dusun
		if err := rows.Scan(&d.ID, &d.Nama); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) AddDusun(nama string) error {
	_, err := s.DB.Exec(`INSERT INTO dusun(nama) VALUES(?)`, nama)
	return mapErr(err)
}

func (s *Store) DeleteDusun(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM dusun WHERE id=?`, id)
	return err
}

/* ------------------------------------------------------------------- users */

type User struct {
	ID         int64
	Username   string
	Role       string
	TimsesID   sql.NullInt64
	TimsesNama string
	Aktif      bool
	MustChange bool
	CreatedAt  string
	PassHash   string
}

const userCols = `u.id, u.username, u.role, u.timses_id, COALESCE(t.nama,''), u.aktif, u.must_change, u.created_at, u.pass_hash`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Role, &u.TimsesID, &u.TimsesNama, &u.Aktif, &u.MustChange, &u.CreatedAt, &u.PassHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) UserByUsername(username string) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users u
		LEFT JOIN timses t ON t.id=u.timses_id WHERE u.username=?`, username))
}

func (s *Store) UserByID(id int64) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users u
		LEFT JOIN timses t ON t.id=u.timses_id WHERE u.id=?`, id))
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.DB.Query(`SELECT ` + userCols + ` FROM users u
		LEFT JOIN timses t ON t.id=u.timses_id ORDER BY u.role, u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(username, hash, role string, timsesID *int64, mustChange bool) (int64, error) {
	var tid any
	if timsesID != nil {
		tid = *timsesID
	}
	res, err := s.DB.Exec(`INSERT INTO users(username,pass_hash,role,timses_id,aktif,must_change,created_at)
		VALUES(?,?,?,?,1,?,?)`, username, hash, role, tid, mustChange, Now())
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

func (s *Store) SetPassword(id int64, hash string, mustChange bool) error {
	_, err := s.DB.Exec(`UPDATE users SET pass_hash=?, must_change=? WHERE id=?`, hash, mustChange, id)
	return err
}

func (s *Store) SetUserAktif(id int64, aktif bool) error {
	_, err := s.DB.Exec(`UPDATE users SET aktif=? WHERE id=?`, aktif, id)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CountAdmins() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND aktif=1`).Scan(&n)
	return n, err
}

func (s *Store) DeleteSessionsByUser(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	return err
}

func (s *Store) DeleteSessionsByTimses(timsesID int64) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE timses_id=?)`, timsesID)
	return err
}

/* ---------------------------------------------------------------- sessions */

type SessionData struct {
	TokenHash   string
	CSRF        string
	ExpiresAt   time.Time
	UserID      int64
	Username    string
	Role        string
	TimsesID    sql.NullInt64
	TimsesNama  string
	MustChange  bool
	UserAktif   bool
	TimsesAktif bool
}

func (s *Store) CreateSession(tokenHash, csrf string, userID int64, expires time.Time) error {
	_, err := s.DB.Exec(`INSERT INTO sessions(token_hash,csrf,user_id,expires_at,created_at) VALUES(?,?,?,?,?)`,
		tokenHash, csrf, userID, expires.UTC().Format(time.RFC3339), Now())
	return err
}

func (s *Store) SessionByToken(tokenHash string) (*SessionData, error) {
	var d SessionData
	var exp string
	err := s.DB.QueryRow(`SELECT s.token_hash, s.csrf, s.expires_at, u.id, u.username, u.role,
			u.timses_id, COALESCE(t.nama,''), u.must_change, u.aktif, COALESCE(t.aktif,1)
		FROM sessions s
		JOIN users u ON u.id=s.user_id
		LEFT JOIN timses t ON t.id=u.timses_id
		WHERE s.token_hash=?`, tokenHash).
		Scan(&d.TokenHash, &d.CSRF, &exp, &d.UserID, &d.Username, &d.Role,
			&d.TimsesID, &d.TimsesNama, &d.MustChange, &d.UserAktif, &d.TimsesAktif)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.ExpiresAt, _ = time.Parse(time.RFC3339, exp)
	return &d, nil
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

func (s *Store) TouchSession(tokenHash string, expires time.Time) error {
	_, err := s.DB.Exec(`UPDATE sessions SET expires_at=? WHERE token_hash=?`,
		expires.UTC().Format(time.RFC3339), tokenHash)
	return err
}

func (s *Store) PurgeSessions() error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE expires_at < ?`, Now())
	return err
}

/* ----------------------------------------------------------------- timses */

type Timses struct {
	ID     int64
	Nama   string
	Kuota  sql.NullInt64
	Aktif  bool
	Terisi int
}

func (t Timses) KuotaEfektif(def int) int {
	if t.Kuota.Valid && t.Kuota.Int64 > 0 {
		return int(t.Kuota.Int64)
	}
	return def
}

const timsesCols = `t.id, t.nama, t.kuota, t.aktif,
	(SELECT COUNT(*) FROM voters v WHERE v.timses_id=t.id)`

func scanTimses(row interface{ Scan(...any) error }) (*Timses, error) {
	var t Timses
	err := row.Scan(&t.ID, &t.Nama, &t.Kuota, &t.Aktif, &t.Terisi)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (s *Store) ListTimses() ([]Timses, error) {
	rows, err := s.DB.Query(`SELECT ` + timsesCols + ` FROM timses t ORDER BY t.nama`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Timses
	for rows.Next() {
		t, err := scanTimses(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) TimsesByID(id int64) (*Timses, error) {
	return scanTimses(s.DB.QueryRow(`SELECT `+timsesCols+` FROM timses t WHERE t.id=?`, id))
}

func (s *Store) CreateTimses(nama string, kuota *int64) (int64, error) {
	var k any
	if kuota != nil {
		k = *kuota
	}
	res, err := s.DB.Exec(`INSERT INTO timses(nama,kuota,aktif,created_at) VALUES(?,?,1,?)`, nama, k, Now())
	if err != nil {
		return 0, mapErr(err)
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTimses(id int64, nama string, kuota *int64, aktif bool) error {
	var k any
	if kuota != nil && *kuota > 0 {
		k = *kuota
	}
	_, err := s.DB.Exec(`UPDATE timses SET nama=?, kuota=?, aktif=? WHERE id=?`, nama, k, aktif, id)
	return mapErr(err)
}

func (s *Store) DeleteTimses(id int64) error {
	n, err := s.CountVotersByTimses(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrTerpakai
	}
	if _, err := s.DB.Exec(`DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE timses_id=?)`, id); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`DELETE FROM users WHERE timses_id=?`, id); err != nil {
		return err
	}
	_, err = s.DB.Exec(`DELETE FROM timses WHERE id=?`, id)
	return err
}

func (s *Store) CountVotersByTimses(id int64) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM voters WHERE timses_id=?`, id).Scan(&n)
	return n, err
}

func (s *Store) CountAllVoters() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM voters`).Scan(&n)
	return n, err
}

func (s *Store) CountVotersByUser(id int64) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM voters WHERE created_by=?`, id).Scan(&n)
	return n, err
}

/* ----------------------------------------------------------------- voters */

type Voter struct {
	ID         int64
	TimsesID   int64
	TimsesNama string
	NoUrut     int
	NIK        string
	Nama       string
	Dusun      string
	RT         string
	RW         string
	CreatedAt  string
	UpdatedAt  string
}

type VoterInput struct {
	Nama  string
	NIK   string
	Dusun string
	RT    string
	RW    string
}

const voterCols = `v.id, v.timses_id, COALESCE(t.nama,''), v.no_urut, v.nik, v.nama, v.dusun, v.rt, v.rw, v.created_at, v.updated_at`

func scanVoter(row interface{ Scan(...any) error }) (*Voter, error) {
	var v Voter
	err := row.Scan(&v.ID, &v.TimsesID, &v.TimsesNama, &v.NoUrut, &v.NIK, &v.Nama,
		&v.Dusun, &v.RT, &v.RW, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &v, err
}

func (s *Store) ListVoters(timsesID *int64, q string) ([]Voter, error) {
	query := `SELECT ` + voterCols + ` FROM voters v LEFT JOIN timses t ON t.id=v.timses_id WHERE 1=1`
	args := []any{}
	if timsesID != nil {
		query += ` AND v.timses_id=?`
		args = append(args, *timsesID)
	}
	if q != "" {
		query += ` AND (v.nama LIKE ? OR v.nik LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if timsesID != nil {
		query += ` ORDER BY v.no_urut`
	} else {
		query += ` ORDER BY t.nama, v.no_urut`
	}

	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Voter
	for rows.Next() {
		v, err := scanVoter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (s *Store) VoterByID(id int64) (*Voter, error) {
	return scanVoter(s.DB.QueryRow(`SELECT `+voterCols+` FROM voters v
		LEFT JOIN timses t ON t.id=v.timses_id WHERE v.id=?`, id))
}

func voterRingkas(v *Voter) string {
	return fmt.Sprintf("%s | %s | %s | RT %s/RW %s", v.Nama, v.NIK, v.Dusun, v.RT, v.RW)
}

func (s *Store) CreateVoter(timsesID int64, in VoterInput, by int64, byName string) (*Voter, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var next int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(no_urut),0)+1 FROM voters WHERE timses_id=?`, timsesID).Scan(&next); err != nil {
		return nil, err
	}
	now := Now()
	res, err := tx.Exec(`INSERT INTO voters(timses_id,no_urut,nik,nama,dusun,rt,rw,created_by,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		timsesID, next, in.NIK, in.Nama, in.Dusun, in.RT, in.RW, by, now, now)
	if err != nil {
		return nil, mapErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	v := &Voter{
		ID: id, TimsesID: timsesID, NoUrut: next, NIK: in.NIK, Nama: in.Nama,
		Dusun: in.Dusun, RT: in.RT, RW: in.RW, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := tx.Exec(`INSERT INTO audit_log(at,user_id,username,aksi,voter_id,ringkas) VALUES(?,?,?,?,?,?)`,
		now, by, byName, "tambah", id, voterRingkas(v)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Store) UpdateVoter(id int64, in VoterInput, by int64, byName string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	old, err := scanVoter(tx.QueryRow(`SELECT `+voterCols+` FROM voters v
		LEFT JOIN timses t ON t.id=v.timses_id WHERE v.id=?`, id))
	if err != nil {
		return err
	}
	now := Now()
	if _, err := tx.Exec(`UPDATE voters SET nik=?, nama=?, dusun=?, rt=?, rw=?, updated_at=? WHERE id=?`,
		in.NIK, in.Nama, in.Dusun, in.RT, in.RW, now, id); err != nil {
		return mapErr(err)
	}
	newV := *old
	newV.NIK, newV.Nama, newV.Dusun, newV.RT, newV.RW = in.NIK, in.Nama, in.Dusun, in.RT, in.RW
	ringkas := "lama: " + voterRingkas(old) + " -> baru: " + voterRingkas(&newV)
	if _, err := tx.Exec(`INSERT INTO audit_log(at,user_id,username,aksi,voter_id,ringkas) VALUES(?,?,?,?,?,?)`,
		now, by, byName, "ubah", id, ringkas); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteVoter(id int64, by int64, byName string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	old, err := scanVoter(tx.QueryRow(`SELECT `+voterCols+` FROM voters v
		LEFT JOIN timses t ON t.id=v.timses_id WHERE v.id=?`, id))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM voters WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO audit_log(at,user_id,username,aksi,voter_id,ringkas) VALUES(?,?,?,?,?,?)`,
		Now(), by, byName, "hapus", id, voterRingkas(old)); err != nil {
		return err
	}
	return tx.Commit()
}

/* ------------------------------------------------------------------ audit */

type AuditEntry struct {
	ID       int64
	At       string
	Username string
	Aksi     string
	VoterID  int64
	Ringkas  string
}

// Log mencatat aksi administratif (bukan perubahan data pemilih).
func (s *Store) Log(userID int64, username, aksi, ringkas string) error {
	_, err := s.DB.Exec(`INSERT INTO audit_log(at,user_id,username,aksi,voter_id,ringkas)
		VALUES(?,?,?,?,NULL,?)`, Now(), userID, username, aksi, ringkas)
	return err
}

func (s *Store) ListAudit(limit int) ([]AuditEntry, error) {
	rows, err := s.DB.Query(`SELECT id, at, COALESCE(username,''), aksi, COALESCE(voter_id,0), ringkas
		FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.At, &a.Username, &a.Aksi, &a.VoterID, &a.Ringkas); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
