const root = document.getElementById("root");
const outDir = document.getElementById("outDir");
const apiKey = document.getElementById("apiKey");
const start = document.getElementById("start");
const cancel = document.getElementById("cancel");
const status = document.getElementById("status");
const log = document.getElementById("log");

function appendLog(line) {
  log.textContent += line + "\n";
  log.scrollTop = log.scrollHeight;
}

function handleProgress(line) {
  appendLog(line);
  if (line === "DUPLICATE_SCAN=START") {
    status.textContent = "重複ファイルを確認中…";
  } else if (line.startsWith("TITLES_UNIQUE=")) {
    status.textContent = "作品を検索・判定中…";
  } else if (line.startsWith("PROGRESS=")) {
    const m = line.match(/PROGRESS=(\d+)\/(\d+)/);
    if (m) status.textContent = `処理中… ${m[1]} / ${m[2]}`;
  } else if (line === "完了しました。") {
    status.textContent = "完了しました。";
  } else if (line === "キャンセルしました。") {
    status.textContent = "キャンセルしました。";
  }
}

async function init() {
  const s = await window.go.main.guiApp.GetSettings();
  root.value = s.last_root || "";
  outDir.value = s.output_dir || "";
  apiKey.placeholder = s.has_api_key ? "保存済み（変更時だけ入力）" : "初回のみ入力";
  if (window.runtime?.EventsOn) {
    window.runtime.EventsOn("bulk-progress", handleProgress);
  }
}

document.getElementById("chooseRoot").onclick = async () => {
  const selected = await window.go.main.guiApp.SelectMediaFolder();
  if (selected) root.value = selected;
};

document.getElementById("chooseOut").onclick = async () => {
  const selected = await window.go.main.guiApp.SelectOutputFolder();
  if (selected) outDir.value = selected;
};

document.getElementById("openResults").onclick = async () => {
  await window.go.main.guiApp.OpenResults(outDir.value);
};

start.onclick = async () => {
  start.disabled = true;
  cancel.disabled = false;
  log.textContent = "";
  status.textContent = "処理中…";
  try {
    const result = await window.go.main.guiApp.Start(root.value, outDir.value, apiKey.value);
    status.textContent = result.message;
    if (result.success) {
      apiKey.value = "";
      appendLog("完了: " + result.output_dir);
    }
  } catch (e) {
    status.textContent = "エラー: " + e;
  } finally {
    start.disabled = false;
    cancel.disabled = true;
  }
};

cancel.onclick = async () => {
  cancel.disabled = true;
  status.textContent = "キャンセル中…";
  await window.go.main.guiApp.Cancel();
};

init().catch((e) => {
  status.textContent = "初期化エラー: " + e;
});
