//go:build !linux

package main

import "bufio"

// Di luar Linux, password dibaca apa adanya dari stdin (tanpa sembunyi).
func readSecret(br *bufio.Reader, prompt string) (string, bool, error) {
	s, err := readLine(br)
	return s, false, err
}
