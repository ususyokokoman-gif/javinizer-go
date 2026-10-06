const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const elements = new Map();
const frames=[];
const context = vm.createContext({console, Intl, performance, requestAnimationFrame:(cb)=>frames.push(cb), document: {getElementById(id) { if(!elements.has(id)) elements.set(id,{textContent:'',value:'',classList:{toggle(){}},addEventListener(){}}); return elements.get(id);}},window:{go:{main:{guiApp:{GetSettings:async()=>({})}}}}});
vm.runInContext(fs.readFileSync(__dirname+'/app.js','utf8'),context);
function translate(line) { return vm.runInContext(`humanStatus(${JSON.stringify(line)})`,context); }
for(const [line,want] of [
 ['FILES_TOTAL=1234','動画ファイルを1,234本見つけました'],
 ['TITLES_UNIQUE=1000','検索対象のタイトルは1,000件です'],
 ['TITLES_CACHED=800','過去の結果を800件再利用します'],
 ['time=info msg="LOCAL_TITLE_SEARCH=HIT candidate=IPX-072"','ローカルDBで候補が見つかりました'],
 ['LOCAL_TITLE_SEARCH=MISS','ローカルDBでは見つからないためWeb検索に切り替えます'],
 ['Jev accepted candidate=IPX-072','Jev判定は IPX-072 を支持しました（Jevだけでは自動整理しません）'],
 ['DECISION_RESULT=confirmed candidate=ABC-123','確定しました: ABC-123（自動整理対象）'],
 ['DECISION_RESULT=review candidate=ABC-123','要確認です: 候補 ABC-123（ファイルは変更しません）'],
 ['DECISION_RESULT=unknown candidate=','作品を安全に特定できませんでした（ファイルは変更しません）'],
 ['PROGRESS=250/1000 CONFIRMED=200 REVIEW=30 UNKNOWN=10 ERRORS=10 CACHED=100','250 / 1,000本完了（25%）｜確定 200本・要確認 30本・未特定 10本・エラー 10本・再利用 100本'],
 ['unknown=value',null],
 ['LOCAL_TITLE_SEARCH=MISS reason=jev_rejected candidate=IPX-072','候補をJevが採用しなかったためWeb検索に切り替えます'],
]) test(line,()=>assert.equal(translate(line),want));
test('older WebKit can append progress without Array.at',()=>{
 vm.runInContext('Array.prototype.at = undefined',context);
 assert.doesNotThrow(()=>vm.runInContext('handleProgress("FILES_TOTAL=12")',context));
 assert.match(elements.get('status').textContent,/12本/);
});

test('first paint survives native Start delayed beyond two frames',async()=>{
 await Promise.resolve();
 const reported=[];
 let finish;
 context.window.go.main.guiApp.ReportDisplayTiming=(phase,elapsed)=>reported.push([phase,elapsed]);
 context.window.go.main.guiApp.Start=()=>new Promise(resolve=>{finish=resolve});
 vm.runInContext('renderPending=false',context);
 frames.length=0;
 const running=elements.get('start').onclick();
 for(let i=0;i<2;i++) for(const frame of frames.splice(0)) frame();
 assert.equal(reported.length,0,'measurement must wait until backend trace is ready');
 vm.runInContext('handleProgress("処理を開始します")',context);
 assert.equal(reported.length,1);
 assert.equal(reported[0][0],'first_display');
 assert.ok(reported[0][1]>=0);
 finish({success:true,message:'処理が完了しました。'});
 await running;
});
test('completion elapsed includes time spent in native startup',()=>{
 context.performance={now:()=>5000};
 vm.runInContext('clickStarted=1000;handleProgress("SUMMARY FILES_TOTAL=100 CONFIRMED=80 REVIEW=10 UNKNOWN=7 ERRORS=3 CACHED=80 ELAPSED_MS=500")',context);
 assert.match(elements.get('runSummary').textContent,/経過 4.00秒/);
 assert.match(elements.get('runSummary').textContent,/確定 80本（自動整理対象）/);
 assert.match(elements.get('runSummary').textContent,/要確認 10本/);
 assert.match(elements.get('runSummary').textContent,/未特定 7本/);
 context.performance=performance;
});
