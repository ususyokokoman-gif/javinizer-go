package scrape

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func findHeadlessSearchBrowser() (string, error) {
	for _, name := range []string{"msedge","msedge.exe","google-chrome","google-chrome-stable","chrome","chrome.exe","chromium","chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil && strings.TrimSpace(path) != "" { return path,nil }
	}
	candidates := make([]string,0,16)
	if runtime.GOOS=="windows" {
		for _, root := range []string{os.Getenv("ProgramFiles(x86)"),os.Getenv("ProgramFiles"),os.Getenv("LOCALAPPDATA")} {
			if root=="" { continue }
			candidates=append(candidates,filepath.Join(root,"Microsoft","Edge","Application","msedge.exe"),filepath.Join(root,"Google","Chrome","Application","chrome.exe"))
		}
	}
	if runtime.GOOS=="darwin" {
		candidates=append(candidates,"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge","/Applications/Google Chrome.app/Contents/MacOS/Google Chrome","/Applications/Chromium.app/Contents/MacOS/Chromium")
	}
	if runtime.GOOS=="linux" {
		candidates=append(candidates,"/usr/bin/microsoft-edge","/usr/bin/microsoft-edge-stable","/usr/bin/google-chrome","/usr/bin/google-chrome-stable","/usr/bin/chromium","/usr/bin/chromium-browser")
	}
	for _, candidate := range candidates { if info,err:=os.Stat(candidate); err==nil && !info.IsDir() { return candidate,nil } }
	return "",fmt.Errorf("no supported headless browser found (Edge/Chrome/Chromium)")
}
