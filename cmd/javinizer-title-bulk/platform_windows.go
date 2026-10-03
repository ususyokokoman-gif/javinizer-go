//go:build windows

package main

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32            = windows.NewLazySystemDLL("crypt32.dll")
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procCryptProtect   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect = crypt32.NewProc("CryptUnprotectData")
	procLocalFree      = kernel32.NewProc("LocalFree")
)

func blobFromBytes(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func bytesFromBlob(b dataBlob) []byte {
	if b.cbData == 0 || b.pbData == nil {
		return nil
	}
	return append([]byte(nil), unsafe.Slice(b.pbData, b.cbData)...)
}

func protectSecret(plain string) (string, error) {
	inBytes := []byte(plain)
	in := blobFromBytes(inBytes)
	var out dataBlob
	r, _, callErr := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return "", fmt.Errorf("CryptProtectData: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return base64.StdEncoding.EncodeToString(bytesFromBlob(out)), nil
}

func unprotectSecret(encoded string) (string, error) {
	encrypted, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	in := blobFromBytes(encrypted)
	var out dataBlob
	r, _, callErr := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return "", fmt.Errorf("CryptUnprotectData: %v", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return string(bytesFromBlob(out)), nil
}

func hideCommandWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
}

func openFolder(path string) error {
	return exec.Command("explorer.exe", path).Start()
}
