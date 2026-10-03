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
  if (line.startsWith("TITLE_DB=FIRST_RUN_DOWNLOAD")) {
    status.textContent = "初回のみ：高速タイトルDBを取得中…";
  } else if (line.startsWith("TITLE_DB_DOWNLOAD=") || line.startsWith("TITLE_DB_DOWNLOAD_BYTES=")) {
    status.textContent = "高速タイトルDBをダウンロード中…";
  } else if (line.startsWith("TITLE_DB_IMPORT=START")) {
    status.textContent = "高速タイトルDBを作成中…";
  } else if (line.startsWith("TITLE_DB_INDEX_READY") || line.startsWith("TITLE_DB=READY")) {
    status.textContent = "高速タイトル検索を準備しました。";
  } else if (line.startsWith("TITLE_RESOLUTION=LOCAL_R18")) {
    status.textContent = "ローカルタイトル検索＋Jevで判定中…";
  } else if (line.startsWith("TITLES_UNIQUE=")) {
    status.textContent = "タイトルから品番を検索中…";
  } else if (line.startsWith("PROGRESS=")) {
    const m = line.match(/PROGRESS=(\d+)\/(\d+)/);
    if (m) status.textContent = `タイトル検索中… ${m[1]} / ${m[2]}`;
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
