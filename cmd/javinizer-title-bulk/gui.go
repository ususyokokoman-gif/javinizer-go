package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed gui_frontend/*.html gui_frontend/*.js gui_frontend/*.css
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
	trace   runTrace
}

func runGUI() error {
	frontend, err := fs.Sub(guiAssets, "gui_frontend")
	if err != nil {
		return fmt.Errorf("prepare GUI assets: %w", err)
	}
	app := &guiApp{}
	return wails.Run(&options.App{
		Title:         "JAVINIZER",
		Width:         1000,
		Height:        760,
		MinWidth:      760,
		MinHeight:     640,
		DisableResize: false,
		AssetServer: &assetserver.Options{
			Assets: frontend,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
	})
}

func (a *guiApp) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *guiApp) shutdown(ctx context.Context) {
	a.Cancel()
	a.trace.close()
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
	a.trace.reset()
	began := time.Now()
	a.emitProgress("処理を開始します")
	a.emitTiming(phaseTiming("gui", "first_status", "END", began, began, nil))
	a.emitProgress("設定を確認中")
	configBegan := time.Now()
	a.emitTiming(phaseTiming("gui", "config", "START", configBegan, began, nil))
	defer func() {
		a.mu.Lock()
		a.running = false
		a.cancel = nil
		a.mu.Unlock()
	}()

	runCtx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.cancel = cancel
	a.mu.Unlock()
	defer cancel()
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

	// Keep the full CLI trace on disk while the UI bounds its visible history.
	if err := a.trace.open(filepath.Join(outDir, "run.log")); err != nil {
		return guiRunResult{Message: fmt.Sprintf("詳細ログを保存できません: %v", err)}
	}
	settings, _ := loadGUISettings()
	apiKey = strings.TrimSpace(apiKey)
	suppliedKey := apiKey != ""
	if apiKey == "" && strings.TrimSpace(settings.EncryptedAPIKey) != "" {
		apiKey, err = unprotectSecret(settings.EncryptedAPIKey)
		if err != nil {
			return guiRunResult{Message: "保存済みAPIキーを読み込めません。APIキーを再入力してください。"}
		}
	}
	if apiKey == "" {
		return guiRunResult{Message: "初回のみTypeSafe APIキーを入力してください。"}
	}

	encrypted := settings.EncryptedAPIKey
	// A saved key does not need to be written to Keychain on every Start.
	if suppliedKey || !strings.HasPrefix(encrypted, "keychain:") && goruntime.GOOS == "darwin" {
		encrypted, err = protectSecret(apiKey)
	}
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

	workerCount := "4"
	if goruntime.GOOS == "darwin" {
		workerCount = "8"
	}
	cmd := exec.CommandContext(runCtx, exe,
		"-root", root,
		"-out", outDir,
		"-workers", workerCount,
		"-skip-duplicates",
		"-timeout", "20s",
		"-max-attempts", "1",
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
	a.emitTiming(phaseTiming("gui", "config", "END", configBegan, began, nil))
	childBegan := time.Now()
	a.emitProgress("動画フォルダの処理を準備中")
	a.emitTiming(phaseTiming("gui", "child_start", "START", childBegan, began, nil))
	startErr := cmd.Start()
	a.emitTiming(phaseTiming("gui", "child_start", "END", childBegan, began, startErr))
	if err := startErr; err != nil {
		return guiRunResult{Message: fmt.Sprintf("処理を開始できません: %v", err)}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	var first sync.Once
	onOutput := func() {
		first.Do(func() { a.emitTiming(phaseTiming("gui", "first_cli_output", "END", began, began, nil)) })
	}
	go a.forwardOutput(&wg, stdout, false, onOutput, a.emitProgress)
	go a.forwardOutput(&wg, stderr, true, onOutput, a.emitProgress)
	// Drain both pipes before Wait closes them; otherwise the final summary can be lost.
	wg.Wait()
	err = cmd.Wait()
	if err != nil {
		if runCtx.Err() == context.Canceled {
			a.emitProgress("キャンセルしました。")
			return guiRunResult{Message: "キャンセルしました。", OutputDir: outDir}
		}
		a.emitProgress("処理中にエラーが発生しました。")
		return guiRunResult{Message: fmt.Sprintf("処理に失敗しました: %v", err), OutputDir: outDir}
	}
	a.emitProgress("完了しました。")
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

func (a *guiApp) forwardOutput(wg *sync.WaitGroup, r io.Reader, isErr bool, onOutput func(), emit func(string)) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		onOutput()
		line := scanner.Text()
		if isErr {
			line = "STDERR: " + line
		}
		emit(line)
	}
	if err := scanner.Err(); err != nil {
		emit("ERROR: ログの読み取りに失敗しました: " + err.Error())
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

func (a *guiApp) emitProgress(line string) {
	failed := a.trace.write(line)
	runtime.EventsEmit(a.ctx, "bulk-progress", line)
	if failed {
		runtime.EventsEmit(a.ctx, "bulk-progress", "ERROR: 詳細ログを保存できません")
	}
}

func (a *guiApp) emitTiming(line string) {
	a.emitProgress(line)
	fmt.Println(line)
}

// ReportDisplayTiming receives a monotonic click-to-paint measurement from WebKit.
func (a *guiApp) ReportDisplayTiming(phase string, elapsedMS float64) {
	if phase != "first_display" && phase != "first_processing_display" {
		return
	}
	if math.IsNaN(elapsedMS) || math.IsInf(elapsedMS, 0) || elapsedMS < 0 || elapsedMS > 86400000 {
		return
	}
	a.emitTiming(fmt.Sprintf("TIMING scope=webview phase=%s event=END timestamp=%s elapsed_ms=%.3f", phase, time.Now().UTC().Format(time.RFC3339Nano), elapsedMS))
}
