# KEEPWORDS Windows verification handoff

STATUS: INCOMPLETE

This is the permanent handoff summary for the KEEPWORDS + title-to-catalog-ID packaged Windows EXE work.

## Extended completion criterion — Jev + real-file E2E

The previous Windows verification proves the public-Web resolver and packaged EXE paths only. Product completion now additionally requires:

- title/filename -> public-Web candidate Catalog ID
- Jev System One verification of the selected candidate
- Jev yes-probability threshold: `>= 0.80` accepts; `< 0.80` rejects
- Jev API error / malformed response / enabled-without-key: fail closed
- the packaged Windows EXE must demonstrate the same behavior on actual user file titles, not only the fixed five-title acceptance matrix
- real-file test evidence must record filename/title, Web candidate, provider/result count, Jev probability, adoption/rejection, and final resolved ID
- only after the real-file corpus passes with no unresolved mismatches may overall STATUS return to COMPLETE

Phase 1 (Web search + Gate A/B/C + SHA/distribution identity) remains valid and passed. The overall product is intentionally marked INCOMPLETE until the Jev and real-file gates are proven.


## Final validated source

- repository: `ususyokokoman-gif/javinizer-go`
- branch: `feature/keepwords`
- validated source SHA used to build/test the final EXE: `93b21c54c007ea5ae4bb5c3212c4b5478121e214`

## Acceptance conditions

- [x] Public Web search returns at least one organic result.
- [x] Strict live matrix resolves all five required titles 5/5.
- [x] KEEPWORDS are absent from the actual real-Web search query used by the packaged EXE.
- [x] Gate A: packaged Windows EXE CLI -> KEEPWORDS strip -> public Web -> organic results > 0 -> Catalog ID -> scraper PASS.
- [x] Gate B: same packaged Windows EXE authenticated API -> worker -> KEEPWORDS strip -> public Web -> organic results > 0 -> Catalog ID -> completed job PASS.
- [x] Gate C: same packaged Windows EXE Desktop localhost NoAuth protected API PASS.
- [x] Gate A/B/C use the exact same EXE SHA-256.
- [x] Final distributed EXE SHA-256 equals the tested EXE SHA-256.
- [x] Final EXE SHA-256 differs from the rejected Build #107 hash.
- [x] Distribution ZIP was independently re-downloaded, extracted, and its EXE SHA-256 rechecked.
- [x] Distribution EXE physical MZ-header inspection PASS.
- [x] Unresolved issues: 0.

## Required five-title matrix

1. `SSIS-001` — `一ヶ月間の禁欲の果てに彼女のルームメイト2人と浮気SEXだけに没頭した彼女不在の3日間`
2. `MIDE-007` — `今日、あなたの上司に犯されました。 大橋未久`
3. `IPZ-508` — `背徳の檻 幸せな二組の夫婦を襲う禁断の監禁強制スワッピング凌襲 美波なみ 愛田奈々`
4. `SNIS-323` — `わたし、犯されにゆきます。～弟想いの美しき姉編～`
5. `IPX-072` — `狙われた通学路 共謀痴漢電車 桃乃木かな`

Final result: `LIVE_MATRIX_PASS=5/5`.

## Historical rejected binary

Build #107:
- source head: `490945724e88cbcbc56aae7ce7560be68b10ce1b`
- artifact id: `10301752042`
- EXE SHA-256: `5e11e68b27b3aa75eccb207a503453c6399c15541084269c4ee1f3db8e433197`
- EXE size: 59,096,576 bytes
- ZIP digest: `b2e15c41ff4c343453c55464da52464888a7a3fc35dab98881834865062b355f`
- result: REJECTED by real user test.

## Build #119 root cause

- run: `34784835861`
- job: `103798292341`
- source: `65a4fba83c5a07bd9ed771b1d278139456474eb8`
- evidence artifact: `10326800300`

Gate A correctly failed because the packaged EXE did not prove any organic Web-search result. Google was throttled/interstitialed and DuckDuckGo returned no parseable organic cards. Direct JavDB resolution was explicitly not accepted as proof of public Web search.

## Final self-hosted Windows verification

Runner repository: `ususyokokoman-gif/gpt-windows-validation`  
Runner: `SOPI` / `self-hosted, windows, x64`

Windows Validation:
- run number: `14`
- run id: `36205610753`
- job id: `108301384080`
- overall result: SUCCESS
- strict Web preflight: `provider=duckduckgo organic_results=10`
- matrix: `LIVE_MATRIX_PASS=5/5`
- Gate A: PASS
- Gate B: PASS
- Gate C: PASS
- Gate A organic provider/results: `yahoojp / 10`
- Gate B organic provider/results: `yahoojp / 10`
- KEEPWORDS removed from Gate A/B actual Web query: PASS

Final EXE:
- name: `Javinizer-KEEPWORDS.exe`
- size: `59,160,064` bytes
- SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`

Gate A/B tested SHA, Gate C tested SHA, and built EXE SHA are all exactly the same.

## Distribution

GitHub Release in `ususyokokoman-gif/gpt-windows-validation`:
- release id: `397004142`
- tag: `javinizer-keepwords-selfhosted-36205610753-1`

Direct EXE:
- release asset id: `589576815`
- digest: `sha256:3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`

ZIP:
- release asset / distribution artifact id: `589577138`
- name: `Javinizer-KEEPWORDS-Windows-selfhosted.zip`
- digest: `sha256:d1a204dfcf324f0d116133c2f037164d8d10b06743d4843b94ef011cee1e6c5f`

Actions artifact storage was not used because of the account's Actions/artifact limitation; verified GitHub Release assets were used for the self-hosted distribution.

## Independent re-download verification

- workflow: `Javinizer KEEPWORDS Release Download Verification`
- run number: `1`
- run id: `36206154028`
- job id: `108303017822`
- result: SUCCESS
- downloaded direct EXE SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- downloaded ZIP SHA-256: `d1a204dfcf324f0d116133c2f037164d8d10b06743d4843b94ef011cee1e6c5f`
- downloaded ZIP-contained EXE SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- verification vs distribution SHA: `IDENTICAL_PASS`
- physical inspection: `MZ_PASS`
- old-vs-new hash comparison: `DIFFERENT_PASS`

## Completion

All mandatory source, public-Web, packaged EXE, Desktop NoAuth, same-binary identity, distribution, re-download, and hash-difference checks are satisfied.

STATUS: INCOMPLETE