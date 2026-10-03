package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed gui_frontend/*
var guiAssets embed.FS

type guiSettings struct {
	LastRoot        string `json:"last_root,omitempty"`
	OutputDir       string `json:"output_dir,omitempty"`
	EncryptedAPIKey string `json:"encrypted_api_key,omitempty"`
}

type guiSettingsView struct {
	LastRoot  string `json:"last_root"`
	OutputDir string `json:"output_dir"`
	HasAPIKey bool   `json:"has_api_key"`
}

type guiRunResult struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	OutputDir string `json:"output_dir"`
}

type guiApp struct {
	ctx     context.Context
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
}

func runGUI() error {
	frontend, err := fs.Sub(guiAssets, "gui_frontend")
	if err != nil {
		return fmt.Errorf("prepare GUI assets: %w", err)
	}
	app := &guiApp{}
	return wails.Run(&options.App{
		Title:  "JAVINIZER",
		Width:  760,
		Height: 640,
		AssetServer: &assetserver.Options{
			Assets: frontend,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:      []interface{}{app},
	})
}

func (a *guiApp) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *guiApp) shutdown(ctx context.Context) {
	a.Cancel()
}

func (a *guiApp) SelectMediaFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "動画フォルダを選択",
	})
}

func (a *guiApp) SelectOutputFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "結果の保存先を選択",
	})
}

func (a *guiApp) GetSettings() guiSettingsView {
	s, _ := loadGUISettings()
	out := strings.TrimSpace(s.OutputDir)
	if out == "" {
		out = defaultOutputDir()
	}
	return guiSettingsView{
		LastRoot:  s.LastRoot,
		OutputDir: out,
		HasAPIKey: strings.TrimSpace(s.EncryptedAPIKey) != "",
	}
}

func (a *guiApp) Start(root, outDir, apiKey string) guiRunResult {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return guiRunResult{Message: "すでに処理中です。"}
	}
	a.running = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.running = false
		a.cancel = nil
		a.mu.Unlock()
	}()

	root = strings.TrimSpace(root)
	if root == "" {
		return guiRunResult{Message: "動画フォルダを選択してください。"}
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return guiRunResult{Message: "選択した動画フォルダを開けません。"}
	}
	outDir = strings.TrimSpace(outDir)
	if outDir == "" {
		outDir = defaultOutputDir()
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return guiRunResult{Message: fmt.Sprintf("結果フォルダを作成できません: %v", err)}
	}

	settings, _ := loadGUISettings()
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" && strings.TrimSpace(settings.EncryptedAPIKey) != "" {
		apiKey, err = unprotectSecret(settings.EncryptedAPIKey)
		if err != nil {
			return guiRunResult{Message: "保存済みAPIキーを読み込めません。APIキーを再入力してください。"}
		}
	}
	if apiKey == "" {
		return guiRunResult{Message: "初回のみTypeSafe APIキーを入力してください。"}
	}

	encrypted, err := protectSecret(apiKey)
	if err != nil {
		return guiRunResult{Message: fmt.Sprintf("APIキーを安全に保存できません: %v", err)}
	}
	settings.LastRoot = root
	settings.OutputDir = outDir
	settings.EncryptedAPIKey = encrypted
	if err := saveGUISettings(settings); err != nil {
		return guiRunResult{Message: fmt.Sprintf("設定を保存できません: %v", err)}
	}

	exe, err := os.Executable()
	if err != nil {
		return guiRunResult{Message: fmt.Sprintf("実行ファイルを確認できません: %v", err)}
	}
	runCtx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	defer cancel()

	workerCount := "4"
	if goruntime.GOOS == "darwin" {
		workerCount = "8"
	}
	cmd := exec.CommandContext(runCtx, exe,
		"-root", root,
		"-out", outDir,
		"-workers", workerCount,
		"-timeout", "45s",
		"-max-attempts", "2",
		"-retry-base-delay", "1s",
	)
	cmd.Env = append(os.Environ(), "TYPESAFE_API_KEY="+apiKey)
	hideCommandWindow(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return guiRunResult{Message: fmt.Sprintf("実行準備に失敗しました: %v", err)}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return guiRunResult{Message: fmt.Sprintf("実行準備に失敗しました: %v", err)}
	}
	if err := cmd.Start(); err != nil {
		return guiRunResult{Message: fmt.Sprintf("処理を開始できません: %v", err)}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go a.forwardOutput(&wg, stdout, false)
	go a.forwardOutput(&wg, stderr, true)
	err = cmd.Wait()
	wg.Wait()
	if err != nil {
		if runCtx.Err() == context.Canceled {
			runtime.EventsEmit(a.ctx, "bulk-progress", "キャンセルしました。")
			return guiRunResult{Message: "キャンセルしました。", OutputDir: outDir}
		}
		runtime.EventsEmit(a.ctx, "bulk-progress", "処理中にエラーが発生しました。")
		return guiRunResult{Message: fmt.Sprintf("処理に失敗しました: %v", err), OutputDir: outDir}
	}
	runtime.EventsEmit(a.ctx, "bulk-progress", "完了しました。")
	return guiRunResult{Success: true, Message: "処理が完了しました。", OutputDir: outDir}
}


func (a *guiApp) Cancel() bool {
	a.mu.Lock()
	cancel := a.cancel
	running := a.running
	a.mu.Unlock()
	if running && cancel != nil {
		cancel()
		return true
	}
	return false
}

func (a *guiApp) forwardOutput(wg *sync.WaitGroup, r io.Reader, isErr bool) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if isErr {
			line = "ERROR: " + line
		}
		runtime.EventsEmit(a.ctx, "bulk-progress", line)
	}
}

func (a *guiApp) OpenResults(path string) error {
	if strings.TrimSpace(path) == "" {
		path = defaultOutputDir()
	}
	return openFolder(path)
}

func defaultOutputDir() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = filepath.Dir(os.Args[0])
	}
	return filepath.Join(base, "JAVINIZER", "results")
}

func settingsPath() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = filepath.Dir(os.Args[0])
	}
	return filepath.Join(base, "JAVINIZER", "settings.json")
}

func loadGUISettings() (guiSettings, error) {
	var s guiSettings
	raw, err := os.ReadFile(settingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return guiSettings{}, err
	}
	return s, nil
}

func saveGUISettings(s guiSettings) error {
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
