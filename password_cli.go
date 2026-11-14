package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"datacollection/internal/auth"
	"datacollection/internal/store"
)

var errCancelled = errors.New("dibatalkan")

func readLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return "", errors.New("input berakhir sebelum password dibaca")
		}
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.ContainsRune(line, '\x03') {
		return "", errCancelled
	}
	return line, nil
}

// changePasswordCLI mengganti password satu akun langsung dari database.
// Dipakai saat tidak bisa login, mis. password admin hilang.
func changePasswordCLI(st *store.Store, username string) error {
	u, err := st.UserByUsername(username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("akun %q tidak ditemukan", username)
		}
		return err
	}

	fmt.Fprintf(os.Stderr, "Akun    : %s (%s)\n", u.Username, u.Role)
	if !u.Aktif {
		fmt.Fprintln(os.Stderr, "Catatan : akun ini sedang nonaktif.")
	}

	br := bufio.NewReader(os.Stdin)
	pass, tty, err := readSecret(br, "Password baru : ")
	if err != nil {
		return err
	}
	if len([]rune(pass)) < 8 {
		return auth.ErrWeakPassword
	}
	if tty {
		again, _, err := readSecret(br, "Ulangi password: ")
		if err != nil {
			return err
		}
		if again != pass {
			return errors.New("password tidak sama")
		}
	}

	hash, err := auth.HashPassword(pass)
	if err != nil {
		return fmt.Errorf("gagal membuat hash: %w", err)
	}
	if err := st.SetPassword(u.ID, hash, false); err != nil {
		return fmt.Errorf("gagal menyimpan: %w", err)
	}
	_ = st.DeleteSessionsByUser(u.ID)
	_ = st.Log(0, "cli", "reset", "Password akun "+u.Username+" diganti lewat CLI")

	fmt.Printf("Password akun %s berhasil diganti. Sesi lama dicabut.\n", u.Username)
	return nil
}
