//go:build darwin

package main

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

const macKeychainService = "JAVINIZER TypeSafe API Key"

func protectSecret(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", fmt.Errorf("empty secret")
	}
	account := "JAVINIZER"
	cmd := exec.Command("security", "add-generic-password", "-a", account, "-s", macKeychainService, "-w", plain, "-U")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("store secret in Keychain: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return "keychain:" + account, nil
}

func unprotectSecret(encoded string) (string, error) {
	if strings.HasPrefix(encoded, "keychain:") {
		account := strings.TrimSpace(strings.TrimPrefix(encoded, "keychain:"))
		if account == "" {
			account = "JAVINIZER"
		}
		out, err := exec.Command("security", "find-generic-password", "-a", account, "-s", macKeychainService, "-w").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("read secret from Keychain: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}

	// Migration path from early portable builds that stored base64.
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func hideCommandWindow(cmd *exec.Cmd) {}

func openFolder(path string) error {
	return exec.Command("open", path).Start()
}
