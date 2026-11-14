//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

func ioctlTermios(fd int, req uintptr, t *syscall.Termios) error {
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), req,
		uintptr(unsafe.Pointer(t)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// readSecret membaca satu baris tanpa menampilkan isinya di terminal.
// Nilai kedua true bila input berasal dari terminal (interaktif).
func readSecret(br *bufio.Reader, prompt string) (string, bool, error) {
	fd := int(os.Stdin.Fd())
	var old syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &old); err != nil {
		// Bukan terminal (mis. input dari pipe) — baca apa adanya.
		s, err := readLine(br)
		return s, false, err
	}

	state := old
	// Matikan echo dan ISIG supaya Ctrl+C terbaca sebagai karakter dan
	// terminal selalu dikembalikan ke keadaan semula.
	state.Lflag &^= syscall.ECHO | syscall.ISIG
	if err := ioctlTermios(fd, syscall.TCSETS, &state); err != nil {
		return "", true, fmt.Errorf("gagal mengatur terminal: %w", err)
	}
	fmt.Fprint(os.Stderr, prompt)
	defer func() {
		_ = ioctlTermios(fd, syscall.TCSETS, &old)
		fmt.Fprintln(os.Stderr)
	}()

	s, err := readLine(br)
	return s, true, err
}
