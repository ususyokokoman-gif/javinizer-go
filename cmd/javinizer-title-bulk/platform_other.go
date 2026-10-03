//go:build !windows && !darwin

package main

import (
	"encoding/base64"
	"os/exec"
)

func protectSecret(plain string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(plain)), nil
}

func unprotectSecret(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	return string(raw), err
}

func hideCommandWindow(cmd *exec.Cmd) {}

func openFolder(path string) error {
	return exec.Command("xdg-open", path).Start()
}
