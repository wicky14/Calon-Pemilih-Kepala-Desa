package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	iterations   = 600_000
	saltLen      = 16
	keyLen       = 32
	sessionBytes = 32
	csrfBytes    = 16
)

/* ------------------------------------------------------------- password */

// pbkdf2SHA256 diimplementasikan langsung agar tidak bergantung pada versi
// standar library mana pun.
func pbkdf2SHA256(password, salt []byte, iter, length int) []byte {
	prf := hmac.New(sha256.New, password)
	hLen := prf.Size()
	blocks := (length + hLen - 1) / hLen
	out := make([]byte, 0, blocks*hLen)
	var idx [4]byte

	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(idx[:], uint32(block))
		prf.Write(idx[:])
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)

		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:length]
}

func HashPassword(pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(pw), salt, iterations, keyLen)
	return fmt.Sprintf("pbkdf2$%d$%s$%s",
		iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got := pbkdf2SHA256([]byte(pw), salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var dummyHash, _ = HashPassword("dummy-password-untuk-menyamakan-waktu")

// DummyVerify dipanggil saat username tidak ditemukan, supaya waktu respons
// tidak membocorkan apakah username itu ada atau tidak.
func DummyVerify(pw string) { VerifyPassword(pw, dummyHash) }

/* ---------------------------------------------------------------- tokens */

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func NewSessionToken() string { return randomToken(sessionBytes) }
func NewCSRF() string         { return randomToken(csrfBytes) }

func HashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// RandomPassword menghasilkan password yang mudah dibaca/diketik,
// mis. "k7F2-mQ9x-Tb4p".
func RandomPassword() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	groups := make([]string, 3)
	for g := range groups {
		var sb strings.Builder
		for i := 0; i < 4; i++ {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				panic(err)
			}
			sb.WriteByte(alphabet[n.Int64()])
		}
		groups[g] = sb.String()
	}
	return strings.Join(groups, "-")
}

/* --------------------------------------------------------------- limiter */

type bucket struct {
	fails int
	until time.Time
}

type Limiter struct {
	mu sync.Mutex
	m  map[string]*bucket
}

func NewLimiter() *Limiter { return &Limiter{m: make(map[string]*bucket)} }

const (
	maxFails   = 5
	blockFor   = 5 * time.Minute
	maxBuckets = 5000
)

// Allow mengembalikan false kalau key sedang diblokir.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.m[key]
	if !ok {
		return true
	}
	if !b.until.IsZero() && time.Now().After(b.until) {
		delete(l.m, key)
		return true
	}
	return b.fails < maxFails
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > maxBuckets {
		l.m = make(map[string]*bucket)
	}
	b, ok := l.m[key]
	if !ok {
		b = &bucket{}
		l.m[key] = b
	}
	b.fails++
	if b.fails >= maxFails {
		b.until = time.Now().Add(blockFor)
	}
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}

var ErrWeakPassword = errors.New("password minimal 8 karakter")
