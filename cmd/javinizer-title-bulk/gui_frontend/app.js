const root = document.getElementById("root");
const outDir = document.getElementById("outDir");
const apiKey = document.getElementById("apiKey");
const start = document.getElementById("start");
const status = document.getElementById("status");
const log = document.getElementById("log");

function appendLog(line) {
  log.textContent += line + "\n";
  log.scrollTop = log.scrollHeight;
}

async function init() {
  const s = await window.go.main.guiApp.GetSettings();
  root.value = s.last_root || "";
  outDir.value = s.output_dir || "";
  apiKey.placeholder = s.has_api_key ? "保存済み（変更時だけ入力）" : "初回のみ入力";
  if (window.runtime?.EventsOn) {
    window.runtime.EventsOn("bulk-progress", appendLog);
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
  }
};

init().catch((e) => {
  status.textContent = "初期化エラー: " + e;
});
