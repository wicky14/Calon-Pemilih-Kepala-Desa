package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"datacollection/internal/auth"
	"datacollection/internal/store"
)

/* ------------------------------------------------------------------ types */

type LoginPage struct {
	Base
	Username string
}

type PasswordPage struct {
	Base
	ErrMsg string
}

type VoterForm struct {
	Nama  string
	NIK   string
	Dusun string
	RT    string
	RW    string
}

type VoterErrs struct {
	Nama  string
	NIK   string
	Dusun string
	RT    string
	RW    string
}

func (e VoterErrs) Any() bool {
	return e.Nama != "" || e.NIK != "" || e.Dusun != "" || e.RT != "" || e.RW != ""
}

type FormVM struct {
	Form    VoterForm
	Errs    VoterErrs
	Action  string
	Label   string
	Dusun   []store.Dusun
	Editing bool
	EditID  int64
}

type DashboardPage struct {
	Base
	FormVM
	Voters   []store.Voter
	Terisi   int
	Kuota    int
	Sisa     int
	Persen   int
	Penuh    bool
	Query    string
	FormOpen bool
}

type TimsesRow struct {
	store.Timses
	KuotaEfektif int
	Persen       int
}

type AdminDashPage struct {
	Base
	Rows       []TimsesRow
	Total      int
	TotalKuota int
	Audit      []store.AuditEntry
}

type AdminTimsesPage struct {
	Base
	Rows         []TimsesRow
	DefaultKuota int
	NewUser      string
	NewPass      string
}

type AdminUsersPage struct {
	Base
	Users      []store.User
	TimsesList []store.Timses
	NewUser    string
	NewPass    string
	SelfID     int64
}

type AdminDataPage struct {
	Base
	FormVM
	Voters     []store.Voter
	TimsesList []store.Timses
	Filter     int64
	Query      string
	Total      int
}

type AdminSettingsPage struct {
	Base
	DefaultKuota int
	Dusun        []store.Dusun
}

type AdminIntegrityPage struct {
	Base
	Entries     []store.IntegrityEntry
	JumlahVoter int
}

type AdminAuditPage struct {
	Base
	Entries []store.AuditEntry
}

/* ------------------------------------------------------------------- auth */

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if u := userFrom(r); u != nil {
		if u.Role == "admin" {
			redirect(w, r, "/admin")
		} else {
			redirect(w, r, "/")
		}
		return
	}
	b := s.base(r)
	b.Title = "Masuk"
	s.render(w, r, "login", http.StatusOK, LoginPage{Base: b})
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	renderErr := func(status int, msg string) {
		b := s.base(r)
		b.Title = "Masuk"
		b.Err = msg
		s.render(w, r, "login", status, LoginPage{Base: b, Username: username})
	}

	if username == "" || password == "" {
		renderErr(http.StatusBadRequest, "Username dan password wajib diisi.")
		return
	}

	key := clientIP(r) + "|" + strings.ToLower(username)
	if !s.limiter.Allow(key) {
		renderErr(http.StatusTooManyRequests, "Terlalu banyak percobaan. Coba lagi dalam 5 menit.")
		return
	}

	u, err := s.st.UserByUsername(username)
	if err != nil {
		auth.DummyVerify(password)
		s.limiter.Fail(key)
		renderErr(http.StatusUnauthorized, "Username atau password salah.")
		return
	}
	if !auth.VerifyPassword(password, u.PassHash) {
		s.limiter.Fail(key)
		renderErr(http.StatusUnauthorized, "Username atau password salah.")
		return
	}
	if !u.Aktif {
		renderErr(http.StatusForbidden, "Akun ini dinonaktifkan. Hubungi admin.")
		return
	}
	if u.Role == "timses" {
		if !u.TimsesID.Valid {
			renderErr(http.StatusForbidden, "Akun ini belum terhubung ke timses.")
			return
		}
		t, err := s.st.TimsesByID(u.TimsesID.Int64)
		if err != nil || !t.Aktif {
			renderErr(http.StatusForbidden, "Timses ini dinonaktifkan. Hubungi admin.")
			return
		}
	}

	token := auth.NewSessionToken()
	hash := auth.HashToken(token)
	csrf := auth.NewCSRF()
	_ = s.st.PurgeSessions()
	if err := s.st.CreateSession(hash, csrf, u.ID, time.Now().Add(sessionTTL)); err != nil {
		renderErr(http.StatusInternalServerError, "Gagal membuat sesi. Coba lagi.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	s.limiter.Reset(key)

	if u.Role == "admin" {
		redirect(w, r, "/admin")
	} else {
		redirect(w, r, "/")
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if u := userFrom(r); u != nil {
		_ = s.st.DeleteSession(u.TokenHash)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	redirect(w, r, "/login")
}

func (s *Server) passwordForm(w http.ResponseWriter, r *http.Request) {
	b := s.base(r)
	b.Title = "Ganti Password"
	s.render(w, r, "password", http.StatusOK, PasswordPage{Base: b})
}

func (s *Server) passwordPost(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cur := r.PostFormValue("current")
	nw := r.PostFormValue("new")
	nw2 := r.PostFormValue("confirm")

	fail := func(msg string) {
		b := s.base(r)
		b.Title = "Ganti Password"
		s.render(w, r, "password", http.StatusBadRequest, PasswordPage{Base: b, ErrMsg: msg})
	}

	full, err := s.st.UserByUsername(u.Username)
	if err != nil || !auth.VerifyPassword(cur, full.PassHash) {
		fail("Password saat ini salah.")
		return
	}
	if utf8.RuneCountInString(nw) < 8 {
		fail("Password baru minimal 8 karakter.")
		return
	}
	if nw != nw2 {
		fail("Konfirmasi password tidak sama.")
		return
	}
	hash, err := auth.HashPassword(nw)
	if err != nil {
		fail("Gagal menyimpan password.")
		return
	}
	if err := s.st.SetPassword(u.ID, hash, false); err != nil {
		fail("Gagal menyimpan password.")
		return
	}
	redirect(w, r, "/?ok="+url.QueryEscape("Password berhasil diganti."))
}

/* -------------------------------------------------------------- dashboard */

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.Role == "admin" {
		redirect(w, r, "/admin")
		return
	}
	if u.TimsesID == nil {
		s.fail(w, r, http.StatusForbidden, "Akun ini belum terhubung ke timses.")
		return
	}

	var form VoterForm
	var errs VoterErrs
	var editing bool
	var editID int64
	formOpen := false

	if v := r.URL.Query().Get("edit"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			voter, err := s.st.VoterByID(id)
			if err == nil && voter.TimsesID == *u.TimsesID {
				form = VoterForm{Nama: voter.Nama, NIK: voter.NIK, Dusun: voter.Dusun, RT: voter.RT, RW: voter.RW}
				editing, editID, formOpen = true, id, true
			}
		}
	}

	p, err := s.dashboardData(r, u, form, errs, formOpen, editing, editID)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
		return
	}
	s.render(w, r, "dashboard", http.StatusOK, p)
}

func (s *Server) dashboardData(r *http.Request, u *SessionUser, form VoterForm, errs VoterErrs, open, editing bool, editID int64) (*DashboardPage, error) {
	t, err := s.st.TimsesByID(*u.TimsesID)
	if err != nil {
		return nil, err
	}
	cfg := s.st.Config()
	kuota := t.KuotaEfektif(cfg.DefaultKuota)
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	voters, err := s.st.ListVoters(u.TimsesID, q)
	if err != nil {
		return nil, err
	}
	dusun, err := s.st.ListDusun()
	if err != nil {
		return nil, err
	}

	b := s.base(r)
	b.Title = "Data Saya"

	p := &DashboardPage{Base: b, Voters: voters, Terisi: t.Terisi, Kuota: kuota, Query: q}
	p.Sisa = max(0, kuota-t.Terisi)
	p.Persen = percent(t.Terisi, kuota)
	p.Penuh = t.Terisi >= kuota
	p.FormVM = FormVM{
		Form: form, Errs: errs, Dusun: dusun,
		Action: "/voters", Label: "Simpan data",
		Editing: editing, EditID: editID,
	}
	if editing {
		p.Action = fmt.Sprintf("/voters/%d", editID)
		p.Label = "Simpan perubahan"
	}
	p.FormOpen = open
	return p, nil
}

func (s *Server) voterCreate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.TimsesID == nil {
		s.fail(w, r, http.StatusForbidden, "Akun ini belum terhubung ke timses.")
		return
	}
	if s.st.Config().Terkunci {
		s.fail(w, r, http.StatusForbidden, "Data sudah dikunci oleh admin. Hubungi admin untuk perubahan.")
		return
	}

	in, errs := s.validateVoter(r)
	if errs.Any() {
		p, err := s.dashboardData(r, u, formFrom(in), errs, true, false, 0)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
			return
		}
		s.render(w, r, "dashboard", http.StatusBadRequest, p)
		return
	}

	_, err := s.st.CreateVoter(*u.TimsesID, in, u.ID, u.Username)
	switch {
	case err == store.ErrNIK:
		errs.NIK = "NIK ini sudah terdaftar pada data pemilih."
	case err == store.ErrKuota:
		p, e := s.dashboardData(r, u, formFrom(in), errs, true, false, 0)
		if e != nil {
			s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
			return
		}
		p.Err = "Kuota timses ini sudah penuh. Hubungi admin bila perlu ditambah."
		s.render(w, r, "dashboard", http.StatusConflict, p)
		return
	case err != nil:
		s.fail(w, r, http.StatusInternalServerError, "Gagal menyimpan data.")
		return
	}
	if errs.Any() {
		p, e := s.dashboardData(r, u, formFrom(in), errs, true, false, 0)
		if e != nil {
			s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
			return
		}
		s.render(w, r, "dashboard", http.StatusBadRequest, p)
		return
	}
	redirect(w, r, "/?ok="+url.QueryEscape("Data pemilih tersimpan."))
}

func (s *Server) voterUpdate(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.TimsesID == nil {
		s.fail(w, r, http.StatusForbidden, "Akun ini belum terhubung ke timses.")
		return
	}
	if s.st.Config().Terkunci {
		s.fail(w, r, http.StatusForbidden, "Data sudah dikunci oleh admin.")
		return
	}
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Data tidak ditemukan.")
		return
	}
	voter, err := s.st.VoterByID(id)
	if err != nil || voter.TimsesID != *u.TimsesID {
		s.fail(w, r, http.StatusNotFound, "Data tidak ditemukan.")
		return
	}

	in, errs := s.validateVoter(r)
	if errs.Any() {
		p, e := s.dashboardData(r, u, formFrom(in), errs, true, true, id)
		if e != nil {
			s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
			return
		}
		s.render(w, r, "dashboard", http.StatusBadRequest, p)
		return
	}
	if err := s.st.UpdateVoter(id, in, u.ID, u.Username); err != nil {
		if err == store.ErrNIK {
			errs.NIK = "NIK ini sudah dipakai data lain."
			p, e := s.dashboardData(r, u, formFrom(in), errs, true, true, id)
			if e != nil {
				s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
				return
			}
			s.render(w, r, "dashboard", http.StatusBadRequest, p)
			return
		}
		s.fail(w, r, http.StatusInternalServerError, "Gagal menyimpan perubahan.")
		return
	}
	redirect(w, r, "/?ok="+url.QueryEscape("Perubahan tersimpan."))
}

func (s *Server) voterDelete(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.TimsesID == nil {
		s.fail(w, r, http.StatusForbidden, "Akun ini belum terhubung ke timses.")
		return
	}
	if s.st.Config().Terkunci {
		s.fail(w, r, http.StatusForbidden, "Data sudah dikunci oleh admin.")
		return
	}
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Data tidak ditemukan.")
		return
	}
	voter, err := s.st.VoterByID(id)
	if err != nil || voter.TimsesID != *u.TimsesID {
		s.fail(w, r, http.StatusNotFound, "Data tidak ditemukan.")
		return
	}
	if err := s.st.DeleteVoter(id, u.ID, u.Username); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal menghapus data.")
		return
	}
	redirect(w, r, "/?ok="+url.QueryEscape("Data dihapus."))
}

/* -------------------------------------------------------------- validation */

func (s *Server) validateVoter(r *http.Request) (store.VoterInput, VoterErrs) {
	var errs VoterErrs
	in := store.VoterInput{
		Nama:  strings.Join(strings.Fields(r.PostFormValue("nama")), " "),
		NIK:   strings.TrimSpace(r.PostFormValue("nik")),
		Dusun: strings.TrimSpace(r.PostFormValue("dusun")),
		RT:    strings.TrimSpace(r.PostFormValue("rt")),
		RW:    strings.TrimSpace(r.PostFormValue("rw")),
	}

	if n := utf8.RuneCountInString(in.Nama); n < 2 || n > 100 {
		errs.Nama = "Nama harus 2–100 karakter."
	}

	if len(in.NIK) != 16 || !allDigits(in.NIK) {
		errs.NIK = "NIK harus 16 angka."
	}

	dusun, _ := s.st.ListDusun()
	valid := false
	for _, d := range dusun {
		if d.Nama == in.Dusun {
			valid = true
			break
		}
	}
	if !valid {
		errs.Dusun = "Pilih dusun dari daftar."
	}

	if v, ok := normalizeRT(in.RT); ok {
		in.RT = v
	} else {
		errs.RT = "RT harus 1–3 angka."
	}
	if v, ok := normalizeRT(in.RW); ok {
		in.RW = v
	} else {
		errs.RW = "RW harus 1–3 angka."
	}
	return in, errs
}

func formFrom(in store.VoterInput) VoterForm {
	return VoterForm{Nama: in.Nama, NIK: in.NIK, Dusun: in.Dusun, RT: in.RT, RW: in.RW}
}

func normalizeRT(v string) (string, bool) {
	if len(v) == 0 || len(v) > 3 || !allDigits(v) {
		return v, false
	}
	for len(v) < 3 {
		v = "0" + v
	}
	return v, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

/* ------------------------------------------------------------------ export */

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)

	var tid *int64
	label := "semua"
	if u.Role == "timses" {
		tid = u.TimsesID
		label = "timses"
		if u.TimsesID != nil {
			if t, err := s.st.TimsesByID(*u.TimsesID); err == nil {
				label = t.Nama
			}
		}
	} else if v := r.URL.Query().Get("timses"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			tid = &id
			if t, err := s.st.TimsesByID(id); err == nil {
				label = t.Nama
			}
		}
	}

	voters, err := s.st.ListVoters(tid, "")
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal menyiapkan export.")
		return
	}

	name := fmt.Sprintf("data-pemilih-%s-%s.csv", sanitizeFile(label), time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Nomor", "Nama", "NIK", "Dusun", "No. RT", "No. RW"})
	for i, v := range voters {
		_ = cw.Write([]string{strconv.Itoa(i + 1), v.Nama, v.NIK, v.Dusun, v.RT, v.RW})
	}
	cw.Flush()
}

func sanitizeFile(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == ' ' || c == '-' || c == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "data"
	}
	return out
}

/* ------------------------------------------------------------------- admin */

func (s *Server) adminDash(w http.ResponseWriter, r *http.Request) {
	cfg := s.st.Config()
	list, err := s.st.ListTimses()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
		return
	}
	rows := make([]TimsesRow, 0, len(list))
	total, totalKuota := 0, 0
	for _, t := range list {
		k := t.KuotaEfektif(cfg.DefaultKuota)
		rows = append(rows, TimsesRow{Timses: t, KuotaEfektif: k, Persen: percent(t.Terisi, k)})
		total += t.Terisi
		totalKuota += k
	}
	audit, _ := s.st.ListAudit(8)

	b := s.base(r)
	b.Title = "Dashboard Admin"
	s.render(w, r, "admin_dash", http.StatusOK, AdminDashPage{
		Base: b, Rows: rows, Total: total, TotalKuota: totalKuota, Audit: audit,
	})
}

func (s *Server) adminTimses(w http.ResponseWriter, r *http.Request) {
	cfg := s.st.Config()
	list, err := s.st.ListTimses()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
		return
	}
	rows := make([]TimsesRow, 0, len(list))
	for _, t := range list {
		k := t.KuotaEfektif(cfg.DefaultKuota)
		rows = append(rows, TimsesRow{Timses: t, KuotaEfektif: k, Persen: percent(t.Terisi, k)})
	}
	b := s.base(r)
	b.Title = "Timses"
	s.render(w, r, "admin_timses", http.StatusOK, AdminTimsesPage{
		Base: b, Rows: rows, DefaultKuota: cfg.DefaultKuota,
		NewUser: r.URL.Query().Get("nu"), NewPass: r.URL.Query().Get("pw"),
	})
}

func (s *Server) adminTimsesCreate(w http.ResponseWriter, r *http.Request) {
	nama := strings.Join(strings.Fields(r.PostFormValue("nama")), " ")
	if nama == "" {
		redirect(w, r, "/admin/timses?err="+url.QueryEscape("Nama timses wajib diisi."))
		return
	}
	kuota, ok := parseKuota(r.PostFormValue("kuota"))
	if !ok {
		redirect(w, r, "/admin/timses?err="+url.QueryEscape("Kuota harus angka 1–1000, atau kosongkan."))
		return
	}

	id, err := s.st.CreateTimses(nama, kuota)
	if err != nil {
		msg := "Gagal menambah timses."
		if err == store.ErrTimsesNama {
			msg = "Nama timses itu sudah ada."
		}
		redirect(w, r, "/admin/timses?err="+url.QueryEscape(msg))
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	if username == "" {
		username = fmt.Sprintf("tim%02d", id)
	}
	pass := r.PostFormValue("password")
	generated := false
	if pass == "" {
		pass = auth.RandomPassword()
		generated = true
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		redirect(w, r, "/admin/timses?err="+url.QueryEscape("Timses dibuat, tetapi akun gagal dibuat."))
		return
	}
	tid := id
	if _, err := s.st.CreateUser(username, hash, "timses", &tid, true); err != nil {
		msg := "Timses dibuat, tetapi username sudah dipakai. Buat akun secara manual di menu Akun."
		if err != store.ErrUsername {
			msg = "Timses dibuat, tetapi akun gagal dibuat."
		}
		redirect(w, r, "/admin/timses?err="+url.QueryEscape(msg))
		return
	}

	msg := fmt.Sprintf("Timses %s dibuat.", nama)
	v := url.Values{}
	v.Set("ok", msg)
	v.Set("nu", username)
	if generated {
		v.Set("pw", pass)
	}
	redirect(w, r, "/admin/timses?"+v.Encode())
}

func (s *Server) adminTimsesUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Timses tidak ditemukan.")
		return
	}
	nama := strings.Join(strings.Fields(r.PostFormValue("nama")), " ")
	if nama == "" {
		redirect(w, r, "/admin/timses?err="+url.QueryEscape("Nama timses wajib diisi."))
		return
	}
	kuota, ok := parseKuota(r.PostFormValue("kuota"))
	if !ok {
		redirect(w, r, "/admin/timses?err="+url.QueryEscape("Kuota tidak valid."))
		return
	}
	aktif := r.PostFormValue("aktif") == "1"

	if err := s.st.UpdateTimses(id, nama, kuota, aktif); err != nil {
		msg := "Gagal menyimpan."
		if err == store.ErrTimsesNama {
			msg = "Nama timses itu sudah ada."
		}
		redirect(w, r, "/admin/timses?err="+url.QueryEscape(msg))
		return
	}
	if !aktif {
		_ = s.st.DeleteSessionsByTimses(id)
	}
	redirect(w, r, "/admin/timses?ok="+url.QueryEscape("Perubahan tersimpan."))
}

func (s *Server) adminTimsesDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Timses tidak ditemukan.")
		return
	}
	if err := s.st.DeleteTimses(id); err != nil {
		msg := "Gagal menghapus timses."
		if err == store.ErrTerpakai {
			msg = "Timses masih punya data pemilih. Hapus datanya lebih dulu."
		}
		redirect(w, r, "/admin/timses?err="+url.QueryEscape(msg))
		return
	}
	redirect(w, r, "/admin/timses?ok="+url.QueryEscape("Timses dihapus."))
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
		return
	}
	list, err := s.st.ListTimses()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
		return
	}
	u := userFrom(r)
	b := s.base(r)
	b.Title = "Akun"
	s.render(w, r, "admin_users", http.StatusOK, AdminUsersPage{
		Base: b, Users: users, TimsesList: list,
		NewUser: r.URL.Query().Get("nu"), NewPass: r.URL.Query().Get("pw"),
		SelfID: u.ID,
	})
}

func (s *Server) adminUsersCreate(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.PostFormValue("username"))
	role := r.PostFormValue("role")
	if role != "admin" {
		role = "timses"
	}
	if username == "" {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Username wajib diisi."))
		return
	}

	var tid *int64
	if role == "timses" {
		id, err := strconv.ParseInt(r.PostFormValue("timses_id"), 10, 64)
		if err != nil || id <= 0 {
			redirect(w, r, "/admin/users?err="+url.QueryEscape("Pilih timses untuk akun ini."))
			return
		}
		tid = &id
	}

	pass := r.PostFormValue("password")
	generated := false
	if pass == "" {
		pass = auth.RandomPassword()
		generated = true
	}
	if utf8.RuneCountInString(pass) < 8 {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Password minimal 8 karakter."))
		return
	}
	hash, err := auth.HashPassword(pass)
	if err != nil {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Gagal membuat akun."))
		return
	}
	if _, err := s.st.CreateUser(username, hash, role, tid, true); err != nil {
		msg := "Gagal membuat akun."
		if err == store.ErrUsername {
			msg = "Username sudah dipakai."
		}
		redirect(w, r, "/admin/users?err="+url.QueryEscape(msg))
		return
	}

	v := url.Values{}
	v.Set("ok", "Akun "+username+" dibuat.")
	v.Set("nu", username)
	if generated {
		v.Set("pw", pass)
	}
	redirect(w, r, "/admin/users?"+v.Encode())
}

func (s *Server) adminUserReset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Akun tidak ditemukan.")
		return
	}
	u, err := s.st.UserByID(id)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "Akun tidak ditemukan.")
		return
	}
	pass := auth.RandomPassword()
	hash, err := auth.HashPassword(pass)
	if err != nil {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Gagal membuat password baru."))
		return
	}
	if err := s.st.SetPassword(id, hash, true); err != nil {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Gagal menyimpan password."))
		return
	}
	_ = s.st.DeleteSessionsByUser(id)

	v := url.Values{}
	v.Set("ok", "Password baru untuk "+u.Username+".")
	v.Set("nu", u.Username)
	v.Set("pw", pass)
	redirect(w, r, "/admin/users?"+v.Encode())
}

func (s *Server) adminUserToggle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Akun tidak ditemukan.")
		return
	}
	me := userFrom(r)
	if id == me.ID {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Tidak bisa menonaktifkan akun sendiri."))
		return
	}
	u, err := s.st.UserByID(id)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "Akun tidak ditemukan.")
		return
	}
	if u.Aktif && u.Role == "admin" {
		if n, _ := s.st.CountAdmins(); n <= 1 {
			redirect(w, r, "/admin/users?err="+url.QueryEscape("Minimal harus ada satu admin aktif."))
			return
		}
	}
	if err := s.st.SetUserAktif(id, !u.Aktif); err != nil {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Gagal mengubah status akun."))
		return
	}
	if u.Aktif {
		_ = s.st.DeleteSessionsByUser(id)
	}
	redirect(w, r, "/admin/users?ok="+url.QueryEscape("Status akun diperbarui."))
}

func (s *Server) adminUserLogout(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Akun tidak ditemukan.")
		return
	}
	_ = s.st.DeleteSessionsByUser(id)
	redirect(w, r, "/admin/users?ok="+url.QueryEscape("Semua sesi akun itu dicabut."))
}

func (s *Server) adminUserDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Akun tidak ditemukan.")
		return
	}
	me := userFrom(r)
	if id == me.ID {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Tidak bisa menghapus akun sendiri."))
		return
	}
	u, err := s.st.UserByID(id)
	if err != nil {
		s.fail(w, r, http.StatusNotFound, "Akun tidak ditemukan.")
		return
	}
	if u.Role == "admin" {
		if n, _ := s.st.CountAdmins(); n <= 1 {
			redirect(w, r, "/admin/users?err="+url.QueryEscape("Minimal harus ada satu admin aktif."))
			return
		}
	}
	if n, _ := s.st.CountVotersByUser(id); n > 0 {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Akun ini pernah menginput data. Nonaktifkan saja agar riwayat tetap utuh."))
		return
	}
	if err := s.st.DeleteUser(id); err != nil {
		redirect(w, r, "/admin/users?err="+url.QueryEscape("Gagal menghapus akun."))
		return
	}
	redirect(w, r, "/admin/users?ok="+url.QueryEscape("Akun dihapus."))
}

/* ------------------------------------------------------------------- data */

func (s *Server) adminData(w http.ResponseWriter, r *http.Request) {
	p, err := s.adminDataPage(r)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
		return
	}
	s.render(w, r, "admin_data", http.StatusOK, p)
}

func (s *Server) adminDataPage(r *http.Request) (*AdminDataPage, error) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var filter int64
	if v := r.URL.Query().Get("timses"); v != "" {
		filter, _ = strconv.ParseInt(v, 10, 64)
	}

	var tid *int64
	if filter > 0 {
		tid = &filter
	}
	voters, err := s.st.ListVoters(tid, q)
	if err != nil {
		return nil, err
	}
	list, err := s.st.ListTimses()
	if err != nil {
		return nil, err
	}
	dusun, err := s.st.ListDusun()
	if err != nil {
		return nil, err
	}

	b := s.base(r)
	b.Title = "Semua Data"

	p := &AdminDataPage{Base: b, Voters: voters, TimsesList: list, Filter: filter, Query: q, Total: len(voters)}
	p.FormVM = FormVM{Action: "/admin/data", Label: "Simpan data", Dusun: dusun}

	if v := r.URL.Query().Get("edit"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			if voter, err := s.st.VoterByID(id); err == nil {
				p.Form = VoterForm{Nama: voter.Nama, NIK: voter.NIK, Dusun: voter.Dusun, RT: voter.RT, RW: voter.RW}
				p.Editing = true
				p.EditID = id
				p.Action = fmt.Sprintf("/admin/data/%d", id)
				p.Label = "Simpan perubahan"
			}
		}
	}
	return p, nil
}

func (s *Server) adminDataUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Data tidak ditemukan.")
		return
	}
	u := userFrom(r)
	in, errs := s.validateVoter(r)
	if errs.Any() {
		p, err := s.adminDataPage(r)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, "Gagal memuat data.")
			return
		}
		p.Errs = errs
		p.Form = VoterForm{Nama: in.Nama, NIK: in.NIK, Dusun: in.Dusun, RT: in.RT, RW: in.RW}
		p.Editing = true
		p.EditID = id
		p.Action = fmt.Sprintf("/admin/data/%d", id)
		p.Label = "Simpan perubahan"
		s.render(w, r, "admin_data", http.StatusBadRequest, p)
		return
	}
	if err := s.st.UpdateVoter(id, in, u.ID, u.Username); err != nil {
		msg := "Gagal menyimpan perubahan."
		if err == store.ErrNIK {
			msg = "NIK itu sudah dipakai data lain."
		}
		redirect(w, r, "/admin/data?err="+url.QueryEscape(msg))
		return
	}
	redirect(w, r, "/admin/data?ok="+url.QueryEscape("Perubahan tersimpan."))
}

func (s *Server) adminDataDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Data tidak ditemukan.")
		return
	}
	u := userFrom(r)
	if err := s.st.DeleteVoter(id, u.ID, u.Username); err != nil {
		redirect(w, r, "/admin/data?err="+url.QueryEscape("Gagal menghapus data."))
		return
	}
	redirect(w, r, "/admin/data?ok="+url.QueryEscape("Data dihapus."))
}

/* ------------------------------------------------------------- pengaturan */

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.st.Config()
	dusun, err := s.st.ListDusun()
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat pengaturan.")
		return
	}
	b := s.base(r)
	b.Title = "Pengaturan"
	s.render(w, r, "admin_settings", http.StatusOK, AdminSettingsPage{
		Base: b, DefaultKuota: cfg.DefaultKuota, Dusun: dusun,
	})
}

func (s *Server) adminSettingsSave(w http.ResponseWriter, r *http.Request) {
	nama := strings.Join(strings.Fields(r.PostFormValue("nama_desa")), " ")
	if nama == "" {
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape("Nama desa wajib diisi."))
		return
	}
	kuota, ok := parseKuota(r.PostFormValue("default_kuota"))
	if !ok || kuota == nil {
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape("Kuota default harus angka 1–1000."))
		return
	}
	if err := s.st.SetSetting("nama_desa", nama); err != nil {
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape("Gagal menyimpan."))
		return
	}
	if err := s.st.SetSetting("default_kuota", strconv.FormatInt(*kuota, 10)); err != nil {
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape("Gagal menyimpan."))
		return
	}
	redirect(w, r, "/admin/pengaturan?ok="+url.QueryEscape("Pengaturan tersimpan."))
}

func (s *Server) adminDusunAdd(w http.ResponseWriter, r *http.Request) {
	nama := strings.Join(strings.Fields(r.PostFormValue("nama")), " ")
	if nama == "" {
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape("Nama dusun wajib diisi."))
		return
	}
	if err := s.st.AddDusun(nama); err != nil {
		msg := "Gagal menambah dusun."
		if err == store.ErrDusunExists {
			msg = "Dusun itu sudah ada."
		}
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape(msg))
		return
	}
	redirect(w, r, "/admin/pengaturan?ok="+url.QueryEscape("Dusun ditambahkan."))
}

func (s *Server) adminDusunDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, http.StatusNotFound, "Dusun tidak ditemukan.")
		return
	}
	if err := s.st.DeleteDusun(id); err != nil {
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape("Gagal menghapus dusun."))
		return
	}
	redirect(w, r, "/admin/pengaturan?ok="+url.QueryEscape("Dusun dihapus."))
}

func (s *Server) adminKunci(w http.ResponseWriter, r *http.Request) {
	cfg := s.st.Config()
	next := "1"
	msg := "Data dikunci. Timses tidak bisa menambah atau mengubah data."
	if cfg.Terkunci {
		next = "0"
		msg = "Kunci dibuka. Timses bisa mengubah data lagi."
	}
	if err := s.st.SetSetting("data_terkunci", next); err != nil {
		redirect(w, r, "/admin/pengaturan?err="+url.QueryEscape("Gagal mengubah status kunci."))
		return
	}
	redirect(w, r, "/admin/pengaturan?ok="+url.QueryEscape(msg))
}

/* -------------------------------------------------------------- integritas */

func (s *Server) adminIntegrity(w http.ResponseWriter, r *http.Request) {
	entries, err := s.st.ListIntegrity(50)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat catatan integritas.")
		return
	}
	n, _ := s.st.CountAllVoters()
	b := s.base(r)
	b.Title = "Integritas Data"
	s.render(w, r, "admin_integrity", http.StatusOK, AdminIntegrityPage{
		Base: b, Entries: entries, JumlahVoter: n,
	})
}

func (s *Server) adminIntegrityRecord(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	e, err := s.st.RecordIntegrity(u.Username)
	if err != nil {
		redirect(w, r, "/admin/integritas?err="+url.QueryEscape("Gagal mencatat sidik jari data."))
		return
	}
	msg := fmt.Sprintf("Sidik jari dicatat: %s… (%d data)", e.Hash[:16], e.Jumlah)
	redirect(w, r, "/admin/integritas?ok="+url.QueryEscape(msg))
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.st.ListAudit(300)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "Gagal memuat log.")
		return
	}
	b := s.base(r)
	b.Title = "Riwayat Perubahan"
	s.render(w, r, "admin_audit", http.StatusOK, AdminAuditPage{Base: b, Entries: entries})
}

/* ------------------------------------------------------------------ utils */

func parseKuota(s string) (*int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 1 || n > 1000 {
		return nil, false
	}
	return &n, true
}

func percent(a, b int) int {
	if b <= 0 {
		return 0
	}
	p := a * 100 / b
	if p > 100 {
		p = 100
	}
	return p
}
