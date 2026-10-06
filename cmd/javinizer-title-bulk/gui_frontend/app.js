const root = document.getElementById("root");
const outDir = document.getElementById("outDir");
const apiKey = document.getElementById("apiKey");
const start = document.getElementById("start");
const cancel = document.getElementById("cancel");
const status = document.getElementById("status");
const log = document.getElementById("log");
const technical = document.getElementById("technicalLog");
const summary = document.getElementById("runSummary");
const humanLines = [];
const technicalLines = [];
let paused = false;
let renderPending = false;
let clickStarted = null;
let firstProcessing = false;
let backendReady = false;
let pendingPaint = [];
const number = (v) => Number(v).toLocaleString("ja-JP");
const token = (line, key) => line.match(new RegExp(`(?:^|\\s)${key}=([\\d.]+)`))?.[1] || "0";

function humanStatus(line) {
  let m;
  if ((m = line.match(/^MEDIA_SCAN_PROGRESS=(\d+)/))) return `動画ファイルを探しています（${number(m[1])}本発見）`;
  if ((m = line.match(/^FILES_TOTAL=(\d+)/))) return `動画ファイルを${number(m[1])}本見つけました`;
  if ((m = line.match(/^TITLES_UNIQUE=(\d+)/))) return `検索対象のタイトルは${number(m[1])}件です`;
  if ((m = line.match(/^TITLES_CACHED=(\d+)/))) return `過去の結果を${number(m[1])}件再利用します`;
  if ((m = line.match(/^PROGRESS=(\d+)\/(\d+)/))) {
    const percent = Number(m[2]) ? Math.round(100 * Number(m[1]) / Number(m[2])) : 0;
    return `${number(m[1])} / ${number(m[2])}本完了（${percent}%）｜確定 ${number(token(line, "CONFIRMED"))}本・要確認 ${number(token(line, "REVIEW"))}本・未特定 ${number(token(line, "UNKNOWN"))}本・エラー ${number(token(line, "ERRORS"))}本・再利用 ${number(token(line, "CACHED"))}本`;
  }
  if (/LOCAL_TITLE_SEARCH=HIT/.test(line)) return "ローカルDBで候補が見つかりました";
  if (/LOCAL_TITLE_SEARCH=MISS/.test(line)) {
    if (/reason=jev_rejected/.test(line)) return "候補をJevが採用しなかったためWeb検索に切り替えます";
    if (/reason=ambiguous|reason=weak_candidate/.test(line)) return "候補を絞りきれないためWeb検索に切り替えます";
    if (/reason=jev_unavailable/.test(line)) return "Jev判定を利用できないためWeb検索に切り替えます";
    return "ローカルDBでは見つからないためWeb検索に切り替えます";
  }
  if (/LOCAL_TITLE_SEARCH=UNAVAILABLE/.test(line)) return "ローカルDBを利用できないためWeb検索に切り替えます";
  if ((m = line.match(/Jev.*accepted.*candidate[=: ]+([A-Z0-9-]+)/i))) return `Jev判定は ${m[1]} を支持しました（Jevだけでは自動整理しません）`;
  if ((m = line.match(/^DECISION_RESULT=(\w+) candidate=([^\s]*)/))) {
    if (m[1] === "confirmed") return `確定しました: ${m[2]}（自動整理対象）`;
    if (m[1] === "review") return m[2] ? `要確認です: 候補 ${m[2]}（ファイルは変更しません）` : "要確認です（ファイルは変更しません）";
    if (m[1] === "unknown") return "作品を安全に特定できませんでした（ファイルは変更しません）";
    return "判定中にエラーが発生しました（ファイルは変更しません）";
  }
  if (/^TIMING .*event=START/.test(line)) {
    const names = {state_load:"過去の検索結果を読み込み中",media_scan:"動画ファイルを探しています",title_grouping:"検索するタイトルを整理中",local_db_open:"ローカルタイトルDBを準備中",first_title_search:"最初のタイトルを検索中",state_checkpoint:"検索結果を保存中"};
    return names[line.match(/phase=(\w+)/)?.[1]] || null;
  }
  if (/TITLE_DB=SKIPPED_ALL_CACHED/.test(line)) return "すべて過去の結果を再利用できるため、タイトル検索を省略します";
  if (/TITLE_DB=FIRST_RUN_DOWNLOAD|TITLE_DB_DOWNLOAD/.test(line)) return "初回用のタイトルDBをダウンロード中";
  if (/TITLE_DB_IMPORT=START/.test(line)) return "ローカルタイトルDBを作成中（初回は時間がかかります）";
  if (/TITLE_DB_INDEX_READY|TITLE_DB=READY/.test(line)) return "ローカルタイトルDBの準備ができました";
  if (/^TITLE_RESOLUTION=LOCAL_R18/.test(line)) return "ローカルDBで安全に確定できるものだけ確定し、必要な場合だけ外部情報を調べます";
  if (/^TITLE_RESOLUTION=WEB_ONLY/.test(line)) return "ローカルDBなしで候補を調べます。外部候補だけでは自動整理しません";
  if (/^RETRY /.test(line)) return "一時的な通信エラーのため、少し待って再試行します";
  if (/^ERROR:|level=error|^STDERR: (?:ERROR|FATAL)/.test(line)) return "エラーが発生しました（詳細ログを確認してください）";
  if (/^(処理|設定|動画フォルダ|完了|キャンセル)/.test(line)) return line;
  return null;
}

function queueRender() {
  if (paused || renderPending) return;
  renderPending = true;
  requestAnimationFrame(() => {
    renderPending = false;
    if (paused) return;
    log.textContent = humanLines.join("\n");
    // Hidden technical logs do not incur layout/render work on every message.
    if (document.getElementById("technicalDetails").open) technical.textContent = technicalLines.join("\n");
    if (document.getElementById("autoScroll").checked) {
      log.scrollTop = log.scrollHeight;
      technical.scrollTop = technical.scrollHeight;
    }
  });
}

function appendLog(line) {
  humanLines.push(`${new Date().toLocaleTimeString("ja-JP")}  ${line}`);
  if (humanLines.length > 3000) humanLines.splice(0, humanLines.length - 3000);
  queueRender();
}

function reportPaint(phase, began) {
  // The second frame observes a paint opportunity after the DOM update.
  requestAnimationFrame(() => requestAnimationFrame(() => {
    if (began !== clickStarted) return;
    const elapsed = performance.now() - began;
    if (backendReady) window.go.main.guiApp.ReportDisplayTiming?.(phase, elapsed);
    else pendingPaint.push([phase, elapsed]);
  }));
}

function handleProgress(line) {
  if (line === "処理を開始します") {
    backendReady = true;
    for (const [phase, elapsed] of pendingPaint) window.go.main.guiApp.ReportDisplayTiming?.(phase, elapsed);
    pendingPaint = [];
  }
  technicalLines.push(line);
  if (technicalLines.length > 10000) technicalLines.splice(0, technicalLines.length - 10000);
  const text = humanStatus(line);
  if (text) {
    status.textContent = text;
    if (humanLines[humanLines.length - 1]?.endsWith(`  ${text}`) !== true) appendLog(text);
  }
  if (!firstProcessing && clickStarted !== null && /^(MEDIA_SCAN_PROGRESS=|FILES_TOTAL=|PROGRESS=)/.test(line)) {
    firstProcessing = true;
    reportPaint("first_processing_display", clickStarted);
  }
  if (line.startsWith("SUMMARY ")) {
    const total = token(line,"FILES_TOTAL"), confirmed = token(line,"CONFIRMED"), review = token(line,"REVIEW"), unknown = token(line,"UNKNOWN"), errors = token(line,"ERRORS");
    const elapsedSeconds = clickStarted === null ? Number(token(line,"ELAPSED_MS"))/1000 : (performance.now() - clickStarted)/1000;
    summary.textContent = `動画 ${number(total)}本 ｜ 確定 ${number(confirmed)}本（自動整理対象）｜ 要確認 ${number(review)}本 ｜ 未特定 ${number(unknown)}本 ｜ エラー ${number(errors)}本 ｜ 再利用 ${number(token(line,"CACHED"))}本 ｜ 経過 ${elapsedSeconds.toFixed(2)}秒`;
    summary.hidden = false;
  }
  queueRender();
}

async function init() {
  // Subscribe before loading settings so no early events can be lost.
  window.runtime?.EventsOn?.("bulk-progress", handleProgress);
  const s = await window.go.main.guiApp.GetSettings();
  root.value = s.last_root || "";
  outDir.value = s.output_dir || "";
  apiKey.placeholder = s.has_api_key ? "保存済み（変更時だけ入力）" : "初回のみ入力";
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
  clickStarted = performance.now();
  firstProcessing = false;
  backendReady = false;
  pendingPaint = [];
  start.disabled = true;
  cancel.disabled = false;
  humanLines.length = technicalLines.length = 0;
  log.textContent = technical.textContent = "";
  summary.hidden = true;
  paused = false;
  document.getElementById("pauseLog").textContent = "表示を一時停止";
  status.textContent = "処理を開始します";
  // Synchronous display before the native bridge, Keychain or subprocess work.
  appendLog("処理を開始します");
  log.textContent = humanLines.join("\n");
  reportPaint("first_display", clickStarted);
  try {
    const result = await window.go.main.guiApp.Start(root.value, outDir.value, apiKey.value);
    status.textContent = result.message;
    appendLog(result.message);
    if (result.success) apiKey.value = "";
  } catch (e) {
    status.textContent = "開始できませんでした: " + e;
    appendLog(status.textContent);
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
document.getElementById("expandLog").onclick = () => {
  const expanded = document.getElementById("main").classList.toggle("log-only");
  document.getElementById("expandLog").textContent = expanded ? "ログを縮小" : "ログを拡大";
};
document.getElementById("pauseLog").onclick = () => {
  paused = !paused;
  document.getElementById("pauseLog").textContent = paused ? "表示を再開" : "表示を一時停止";
  if (!paused) queueRender();
};
document.getElementById("technicalDetails").ontoggle = queueRender;
document.getElementById("copyLog").onclick = async () => {
  const text = document.getElementById("technicalDetails").open ? technicalLines.join("\n") : humanLines.join("\n");
  try {
    await window.runtime.ClipboardSetText(text);
    document.getElementById("copyLog").textContent = "コピーしました";
  } catch (_) { document.getElementById("copyLog").textContent = "本文を選択してコピーしてください"; }
};
init().catch((e) => { status.textContent = "初期化エラー: " + e; });
