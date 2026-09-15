# KEEPWORDS Windows verification handoff

STATUS: INCOMPLETE

This document is the permanent handoff/evidence record for the KEEPWORDS + title-to-catalog-ID Windows packaged EXE work. Do not change STATUS to COMPLETE until every acceptance condition below has been proven on the packaged/distributed Windows EXE.

## Acceptance conditions

- [ ] Public Web search returns at least one organic result (`results > 0`).
- [x] Source-level strict live matrix has resolved the five required known titles 5/5 (historical evidence below; must remain compatible with final candidate).
- [ ] KEEPWORDS are proven absent from the actual real-Web search query used by the packaged EXE.
- [ ] Gate A: packaged Windows EXE CLI path performs public Web search and proves organic `results > 0`.
- [ ] Gate B: the same packaged Windows EXE receives filename through authenticated HTTP API -> worker -> public Web search and proves organic `results > 0`.
- [ ] Gate C: Desktop localhost NoAuth API passes on the same EXE artifact.
- [ ] Gate A/B/C EXE and distributed EXE are SHA-256 identical.
- [ ] Final EXE SHA-256 differs from the known bad Windows Build #107 hash.
- [ ] Final distributed artifact has been independently downloaded/extracted and its EXE SHA-256 rechecked.

## Required five-title matrix

1. `SSIS-001` — `一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間`
2. `MIDE-007` — `今日、あなたの上司に犯されました。 大橋未久`
3. `IPZ-508` — `背徳の檻 幸せな二組の夫婦を襲う禁断の監禁強制スワッピング凌襲 美波なみ 愛田奈々`
4. `SNIS-323` — `わたし、犯されにゆきます。～弟想いの美しき姉編～`
5. `IPX-072` — `狙われた通学路 共謀痴漢電車 桃乃木かな`

## Known bad artifact — never accept as fixed

Windows Build #107:

- source head: `490945724e88cbcbc56aae7ce7560be68b10ce1b`
- artifact id: `10301752042`
- EXE SHA-256: `5e11e68b27b3aa75eccb207a503453c6399c15541084269c4ee1f3db8e433197`
- EXE size: 59,096,576 bytes
- ZIP digest: `b2e15c41ff4c343453c55464da52464888a7a3fc35dab98881834865062b355f`
- result: REJECTED. The user tested this binary and it did not satisfy the intended behavior.

## Source-level strict-live evidence

Catalog ID Evidence Verification #38:

- run id: `34784835871`
- job id: `103798292368`
- source head: `65a4fba83c5a07bd9ed771b1d278139456474eb8`
- result: SUCCESS
- strict public-Web preflight: DuckDuckGo returned 10 organic results for the first required title after Google was blocked/interstitialed.
- evidence line included `LIVE_WEB_STRICT_PASS provider=duckduckgo organic_results=10`.
- required title matrix: `LIVE_MATRIX_PASS=5/5`.

This proves source-level behavior only. It does NOT satisfy packaged-EXE Gate A/B/C.

## Windows Build #119 — confirmed failure

- run id: `34784835861`
- job id: `103798292341`
- source head: `65a4fba83c5a07bd9ed771b1d278139456474eb8`
- evidence artifact id: `10326800300`
- result: FAILED at the strict packaged Windows EXE black-box step.

### Root cause confirmed from the #119 evidence artifact

Gate A's packaged EXE successfully returned `SSIS-001`, but this was not sufficient for acceptance. During that exact packaged execution:

- Google direct/headless search was blocked/interstitialed/429.
- DuckDuckGo returned no parseable organic results at that moment.
- logs contained public-search fallback failures such as `google returned no parseable results | duckduckgo returned no parseable results`.
- the correct `SSIS-001` was subsequently resolved from direct JavDB evidence.
- therefore no packaged-EXE log line proved `web search provider=... results=[1-9][0-9]*`.

The strict wrapper correctly rejected the run. This was a real runtime search-resilience failure, not a test bug. The gate must not be weakened to accept direct JavDB resolution alone.

## Changes after #119 awaiting packaged verification

Candidate branch head observed before creating this handoff document:

- `6b89b720cdbaef916908ec4ba5587de96f1bd9f1`

Relevant changes present at that candidate include:

- DuckDuckGo retry support.
- Bing public-search fallback after Google and DuckDuckGo.
- Bing retry support.
- strict KEEPWORDS assertion scoped to the extracted search query rather than blindly scanning an entire log line (avoids false positives such as short configured token `AI` occurring outside the query).

These changes are NOT accepted merely because they exist in source. A packaged Windows EXE must pass all gates above.

## Relevant files

- `internal/scrape/title_web_lookup.go`
- `internal/scrape/title_web_fallback.go`
- `internal/scrape/title_web_browser.go`
- `internal/scrape/title_web_browser_find.go`
- `internal/scrape/title_web_search_live_contract_test.go`
- `internal/scrape/title_web_search_keepwords_test.go`
- `.github/scripts/verify_keepwords_windows_blackbox.ps1`
- `.github/workflows/keepwords-windows.yml`
- `.github/workflows/catalog-id-evidence.yml`
- `.github/workflows/keepwords-desktop-noauth-windows.yml`

## Final evidence fields — fill only after verification

- final source commit: PENDING
- Windows Build run number/id: PENDING
- Windows Build job id: PENDING
- Gate A organic provider/results: PENDING
- Gate A KEEPWORDS query-clean proof: PENDING
- Gate B organic provider/results: PENDING
- Gate B KEEPWORDS query-clean proof: PENDING
- Gate C result: PENDING
- final five-title matrix: PENDING
- final EXE SHA-256: PENDING
- distributed artifact id: PENDING
- independently downloaded EXE SHA-256: PENDING
- exact-hash equality across Gate A/B/C/distribution: PENDING
- old-vs-new hash inequality: PENDING
- unresolved items: packaged verification still required
