package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"datacollection/internal/auth"
	"datacollection/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

const (
	cookieName = "sid"
	sessionTTL = 12 * time.Hour
)

type SessionUser struct {
	ID         int64
	Username   string
	Role       string
	TimsesID   *int64
	TimsesNama string
	MustChange bool
	CSRF       string
	TokenHash  string
}

type ctxKey int

const userKey ctxKey = 1

type Server struct {
	st      *store.Store
	pages   map[string]*template.Template
	limiter *auth.Limiter
	static  http.Handler
}

func New(st *store.Store) (*Server, error) {
	s := &Server{st: st, limiter: auth.NewLimiter(), pages: map[string]*template.Template{}}

	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	s.static = http.StripPrefix("/static/", http.FileServerFS(staticFS))

	pageFiles := map[string]string{
		"login":           "login.html",
		"password":        "password.html",
		"dashboard":       "dashboard.html",
		"admin_dash":      "admin_dash.html",
		"admin_timses":    "admin_timses.html",
		"admin_users":     "admin_users.html",
		"admin_data":      "admin_data.html",
		"admin_settings":  "admin_settings.html",
		"admin_integrity": "admin_integrity.html",
		"admin_audit":     "admin_audit.html",
		"error":           "error.html",
	}
	for name, file := range pageFiles {
		t, err := template.New("layout").Funcs(funcMap()).ParseFS(assets,
			"templates/layout.html", "templates/partials.html", "templates/"+file)
		if err != nil {
			return nil, err
		}
		s.pages[name] = t
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", cacheStatic(s.static))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginPost)
	mux.HandleFunc("POST /logout", s.require("", s.logout))

	mux.HandleFunc("GET /{$}", s.require("", s.dashboard))
	mux.HandleFunc("GET /password", s.require("", s.passwordForm))
	mux.HandleFunc("POST /password", s.require("", s.passwordPost))

	mux.HandleFunc("POST /voters", s.require("timses", s.voterCreate))
	mux.HandleFunc("POST /voters/{id}", s.require("timses", s.voterUpdate))
	mux.HandleFunc("POST /voters/{id}/delete", s.require("timses", s.voterDelete))
	mux.HandleFunc("GET /export.csv", s.require("", s.exportCSV))

	mux.HandleFunc("GET /admin", s.require("admin", s.adminDash))
	mux.HandleFunc("GET /admin/timses", s.require("admin", s.adminTimses))
	mux.HandleFunc("POST /admin/timses", s.require("admin", s.adminTimsesCreate))
	mux.HandleFunc("POST /admin/timses/{id}", s.require("admin", s.adminTimsesUpdate))
	mux.HandleFunc("POST /admin/timses/{id}/delete", s.require("admin", s.adminTimsesDelete))

	mux.HandleFunc("GET /admin/users", s.require("admin", s.adminUsers))
	mux.HandleFunc("POST /admin/users", s.require("admin", s.adminUsersCreate))
	mux.HandleFunc("POST /admin/users/{id}/reset", s.require("admin", s.adminUserReset))
	mux.HandleFunc("POST /admin/users/{id}/toggle", s.require("admin", s.adminUserToggle))
	mux.HandleFunc("POST /admin/users/{id}/logout", s.require("admin", s.adminUserLogout))
	mux.HandleFunc("POST /admin/users/{id}/delete", s.require("admin", s.adminUserDelete))

	mux.HandleFunc("GET /admin/data", s.require("admin", s.adminData))
	mux.HandleFunc("POST /admin/data/{id}", s.require("admin", s.adminDataUpdate))
	mux.HandleFunc("POST /admin/data/{id}/delete", s.require("admin", s.adminDataDelete))

	mux.HandleFunc("GET /admin/pengaturan", s.require("admin", s.adminSettings))
	mux.HandleFunc("POST /admin/pengaturan", s.require("admin", s.adminSettingsSave))
	mux.HandleFunc("POST /admin/dusun", s.require("admin", s.adminDusunAdd))
	mux.HandleFunc("POST /admin/dusun/{id}/delete", s.require("admin", s.adminDusunDelete))
	mux.HandleFunc("POST /admin/kunci", s.require("admin", s.adminKunci))

	mux.HandleFunc("GET /admin/integritas", s.require("admin", s.adminIntegrity))
	mux.HandleFunc("POST /admin/integritas", s.require("admin", s.adminIntegrityRecord))
	mux.HandleFunc("GET /admin/audit", s.require("admin", s.adminAudit))

	return recoverMW(logMW(s.withUser(mux)))
}

/* ------------------------------------------------------------- middleware */

func (s *Server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
			hash := auth.HashToken(c.Value)
			sess, err := s.st.SessionByToken(hash)
			if err == nil && time.Now().Before(sess.ExpiresAt) && sess.UserAktif &&
				(sess.Role == "admin" || sess.TimsesAktif) {
				u := &SessionUser{
					ID: sess.UserID, Username: sess.Username, Role: sess.Role,
					MustChange: sess.MustChange, CSRF: sess.CSRF, TokenHash: hash,
					TimsesNama: sess.TimsesNama,
				}
				if sess.TimsesID.Valid {
					id := sess.TimsesID.Int64
					u.TimsesID = &id
				}
				if time.Until(sess.ExpiresAt) < sessionTTL/2 {
					_ = s.st.TouchSession(hash, time.Now().Add(sessionTTL))
				}
				r = r.WithContext(context.WithValue(r.Context(), userKey, u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) require(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r)
		if u == nil {
			if r.Method == http.MethodGet {
				redirect(w, r, "/login")
			} else {
				s.fail(w, r, http.StatusUnauthorized, "Sesi berakhir. Silakan login ulang.")
			}
			return
		}
		if u.MustChange && r.URL.Path != "/password" && r.URL.Path != "/logout" {
			redirect(w, r, "/password")
			return
		}
		if role != "" && u.Role != role {
			s.fail(w, r, http.StatusForbidden, "Anda tidak punya akses ke halaman ini.")
			return
		}
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				s.fail(w, r, http.StatusBadRequest, "Formulir tidak valid.")
				return
			}
			got := r.PostFormValue("csrf")
			if subtle.ConstantTimeCompare([]byte(got), []byte(u.CSRF)) != 1 {
				s.fail(w, r, http.StatusForbidden, "Token keamanan kedaluwarsa. Muat ulang halaman lalu coba lagi.")
				return
			}
		}
		next(w, r)
	}
}

func recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, v)
				http.Error(w, "Terjadi kesalahan internal.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// Path saja, tidak pernah query string (bisa memuat NIK).
		if strings.HasPrefix(r.URL.Path, "/static/") {
			return
		}
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}

/* ---------------------------------------------------------------- helpers */

func userFrom(r *http.Request) *SessionUser {
	u, _ := r.Context().Value(userKey).(*SessionUser)
	return u
}

func redirect(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

type Base struct {
	Title    string
	User     *SessionUser
	NamaDesa string
	CSRF     string
	Locked   bool
	OK       string
	Err      string
	Path     string
	AdminNav bool
}

func (s *Server) base(r *http.Request) Base {
	cfg := s.st.Config()
	b := Base{
		NamaDesa: cfg.NamaDesa,
		Locked:   cfg.Terkunci,
		Path:     r.URL.Path,
	}
	if u := userFrom(r); u != nil {
		b.User = u
		b.CSRF = u.CSRF
		b.AdminNav = u.Role == "admin"
	}
	q := r.URL.Query()
	b.OK = q.Get("ok")
	b.Err = q.Get("err")
	return b
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, status int, data any) {
	t := s.pages[page]
	if t == nil {
		http.Error(w, "template tidak ditemukan", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

type errorPage struct {
	Base
	Message string
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	b := s.base(r)
	b.Title = "Kesalahan"
	s.render(w, r, "error", status, errorPage{Base: b, Message: msg})
}

/* ------------------------------------------------------------ func map */

var iconPaths = map[string]string{
	"plus":     `<path d="M12 5v14M5 12h14"/>`,
	"search":   `<circle cx="11" cy="11" r="7"/><path d="M20 20l-3.6-3.6"/>`,
	"edit":     `<path d="M4 20h4L18 10l-4-4L4 16v4z"/><path d="M13.5 6.5l4 4"/>`,
	"trash":    `<path d="M4 7h16"/><path d="M9.5 7V5h5v2"/><path d="M6.5 7l1 13h9l1-13"/>`,
	"logout":   `<path d="M15 4h3a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-3"/><path d="M10 8l-4 4 4 4"/><path d="M6 12h9"/>`,
	"download": `<path d="M12 4v11"/><path d="M8 11l4 4 4-4"/><path d="M5 20h14"/>`,
	"lock":     `<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>`,
	"unlock":   `<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 7-2.6"/>`,
	"user":     `<circle cx="12" cy="8" r="3.5"/><path d="M5 20a7 7 0 0 1 14 0"/>`,
	"users":    `<circle cx="9" cy="8" r="3"/><path d="M3 19a6 6 0 0 1 12 0"/><path d="M16 6.5a3 3 0 0 1 0 5.5"/><path d="M18.5 19a6 6 0 0 0-2.5-4.8"/>`,
	"check":    `<path d="M5 13l4 4 10-10"/>`,
	"back":     `<path d="M10 6l-5 6 5 6"/><path d="M5 12h14"/>`,
	"shield":   `<path d="M12 4l7 3v5c0 4-3 6.5-7 8-4-1.5-7-4-7-8V7l7-3z"/><path d="M9 12l2 2 4-4"/>`,
	"key":      `<circle cx="8" cy="15" r="3.5"/><path d="M10.5 12.5L19 4"/><path d="M16 7l2 2"/>`,
	"gear":     `<circle cx="12" cy="12" r="3"/><path d="M12 3v2.2M12 18.8V21M4.9 4.9l1.6 1.6M17.5 17.5l1.6 1.6M3 12h2.2M18.8 12H21M4.9 19.1l1.6-1.6M17.5 6.5l1.6-1.6"/>`,
	"list":     `<path d="M8 6h13M8 12h13M8 18h13"/><path d="M3.5 6h.01M3.5 12h.01M3.5 18h.01"/>`,
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"icon": func(name string) template.HTML {
			p, ok := iconPaths[name]
			if !ok {
				return ""
			}
			return template.HTML(`<svg class="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + p + `</svg>`)
		},
		"maskNIK": func(nik string) string {
			if len(nik) <= 8 {
				return nik
			}
			return nik[:4] + "••••••••" + nik[len(nik)-4:]
		},
		"pct": func(a, b int) int {
			if b <= 0 {
				return 0
			}
			p := a * 100 / b
			if p > 100 {
				p = 100
			}
			return p
		},
		"tanggal": func(s string) string {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return s
			}
			return t.Local().Format("02 Jan 2006 15:04")
		},
		"tanggalPendek": func(s string) string {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return s
			}
			return t.Local().Format("02/01/2006")
		},
		"potong": func(s string, n int) string {
			r := []rune(s)
			if len(r) <= n {
				return s
			}
			return string(r[:n]) + "…"
		},
	}
}
