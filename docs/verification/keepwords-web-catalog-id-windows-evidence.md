# KEEPWORDS / Web Catalog-ID Windows Verification Evidence

STATUS: COMPLETE

## Purpose

Prove KEEPWORDS extraction/retention and title-to-Catalog-ID public-Web resolution in the final Windows executable, not only in source tests.

## Historical defect

- Old bad Build #107 source head: `490945724e88cbcbc56aae7ce7560be68b10ce1b`
- Old bad Build #107 EXE SHA-256: `5e11e68b27b3aa75eccb207a503453c6399c15541084269c4ee1f3db8e433197`
- Old bad Build #107 EXE size: 59,096,576 bytes
- Old bad Build #107 artifact id: `10301752042`
- Old bad Build #107 ZIP digest: `b2e15c41ff4c343453c55464da52464888a7a3fc35dab98881834865062b355f`
- Result: REJECTED by real user test.

Build #119:
- source SHA: `65a4fba83c5a07bd9ed771b1d278139456474eb8`
- run: `34784835861`
- Windows job: `103798292341`
- evidence artifact: `10326800300`
- evidence ZIP digest: `sha256:c7e1a88e1b19a348f5d3ccc2644a9f273c73b7c622be470ea6ce1d6b2bb71081`
- first strict packaged-EXE failure: Gate A did not prove that the built EXE received at least one organic Web-search result.
- confirmed runtime cause: packaged EXE encountered Google throttling/interstitials and DuckDuckGo pages with no parseable organic results; direct JavDB could still identify SSIS-001, but the strict gate correctly rejected that as public-Web proof.

## Remediation history

Relevant commits:
- `6dc67c3f8b4611d2c0b0494783d780da280ebb63` — harden packaged title Web-search fallback.
- `6b89b720cdbaef916908ec4ba5587de96f1bd9f1` — avoid KEEPWORD false-positive evidence checks.
- `84469f8243177ab29a6e2b9cb05f2b4ecc640fb` — add Yahoo Japan organic-search fallback.
- `3c64c4f6ad32b598da4ed18c4184b4e2d6bb8d36` — accept Yahoo Japan as strict organic evidence.
- `ef61120ebc2ecb17b579810acd85d8943a30010e` — add Bing tracking-URL normalization.
- `9b4dd6ff1e66e2d94ed7a72c81816ff88fb053eb` — actually apply Bing URL normalization inside the Bing parser.
- `93b21c54c007ea5ae4bb5c3212c4b5478121e214` — final validated source candidate after Bing parser regression/import cleanup.

A separate diagnostic step was also removed from the final self-hosted validation sequence because it repeatedly queried the same public search providers immediately before the strict five-title contract and caused artificial rate-limit exhaustion. The strict five-title test itself was not weakened.

## Final validated source

- repository: `ususyokokoman-gif/javinizer-go`
- branch: `feature/keepwords`
- validated source SHA used to build/test the distributed EXE: `93b21c54c007ea5ae4bb5c3212c4b5478121e214`

## Self-hosted Windows final validation

Runner repository: `ususyokokoman-gif/gpt-windows-validation`  
Runner: `SOPI`  
Labels: `self-hosted, windows, x64`

Final Windows Validation:
- workflow: `Javinizer KEEPWORDS Self-hosted Windows Validation`
- run number: `14`
- run id: `36205610753`
- job id: `108301384080`
- overall result: SUCCESS

### Deterministic / regression tests

- Desktop localhost NoAuth security invariants: PASS
- Non-desktop loopback still requires authentication: PASS
- KEEPWORDS template tests: PASS
- Deterministic worker -> Web resolver -> scraper E2E: PASS
- Existing Web-first regression tests: PASS
- Frontend build: PASS
- Windows desktop EXE build: PASS

### Strict public-Web evidence

Strict preflight:
- provider: `duckduckgo`
- organic results: `10`
- evidence: `LIVE_WEB_STRICT_PASS provider=duckduckgo organic_results=10`

Five required titles:
- SSIS-001: PASS
- MIDE-007: PASS
- IPZ-508: PASS
- SNIS-323: PASS
- IPX-072: PASS
- matrix evidence: `LIVE_MATRIX_PASS=5/5`

The final run used real public Web providers including DuckDuckGo, Yahoo Japan, and Bing fallback where needed. Direct metadata fallback was not counted as organic-Web success.

## Packaged EXE gates

Exact built EXE:
- name: `Javinizer-KEEPWORDS.exe`
- size: `59,160,064` bytes
- SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`

### Gate A — exact EXE CLI

- result: PASS
- catalog ID: `SSIS-001`
- organic provider: `yahoojp`
- organic results: `10`
- actual public-Web query contained the real title only; configured KEEPWORDS were absent.
- evidence included:
  - web-title cleanup from the KEEPWORDS-bearing filename/title
  - normalized web-title lookup input without KEEPWORDS
  - `web search provider=yahoojp ... results=10`
  - `title web lookup resolved ... -> SSIS-001`

### Gate B — same EXE authenticated API -> worker

- result: PASS
- catalog ID: `SSIS-001`
- organic provider: `yahoojp`
- organic results: `10`
- actual public-Web query contained the real title only; configured KEEPWORDS were absent.
- evidence: `blackbox_verification=PASS`, `gate_a=PASS`, `gate_b=PASS`.

### Gate C — same EXE Desktop localhost NoAuth

- result: PASS
- `auth_status_initialized=True`
- `auth_status_authenticated=True`
- `auth_status_username=local`
- `auth_status_session_id_empty=True`
- protected batch without authentication: PASS
- resolved `movie_id=SSIS-001`
- no setup/login/session/token path was substituted.

### Same-EXE identity

- Gate A/B tested SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- Gate C tested SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- built EXE SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- identity result: `sha256_identity=PASS`

## Distribution

GitHub Release:
- repository: `ususyokokoman-gif/gpt-windows-validation`
- release id: `397004142`
- tag: `javinizer-keepwords-selfhosted-36205610753-1`
- prerelease: yes

Direct EXE asset:
- asset id: `589576815`
- name: `Javinizer-KEEPWORDS.exe`
- size: `59,160,064` bytes
- GitHub digest: `sha256:3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`

Distribution ZIP asset:
- asset id / distribution artifact id: `589577138`
- name: `Javinizer-KEEPWORDS-Windows-selfhosted.zip`
- size: `21,452,391` bytes
- GitHub digest: `sha256:d1a204dfcf324f0d116133c2f037164d8d10b06743d4843b94ef011cee1e6c5f`

The GitHub Actions artifact store was not used because the account Actions/artifact quota was constrained; the self-hosted verified distribution was published as GitHub Release assets instead.

## Independent distribution re-download verification

Workflow:
- `Javinizer KEEPWORDS Release Download Verification`
- run number: `1`
- run id: `36206154028`
- job id: `108303017822`
- result: SUCCESS

The separate job re-downloaded both published Release assets from GitHub and verified:
- downloaded direct EXE SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- downloaded ZIP SHA-256: `d1a204dfcf324f0d116133c2f037164d8d10b06743d4843b94ef011cee1e6c5f`
- EXE extracted from downloaded ZIP SHA-256: `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- verification vs distribution SHA: `IDENTICAL_PASS`
- physical EXE inspection: `MZ_PASS`

## Old-vs-new hash comparison

- old rejected Build #107 EXE:
  `5e11e68b27b3aa75eccb207a503453c6399c15541084269c4ee1f3db8e433197`
- final verified EXE:
  `3cb5c76dee23971c3933083573fd2997f7726717be1281dede454926a63cf00e`
- comparison: `DIFFERENT_PASS`

## Final acceptance

- deterministic tests: PASS
- real public Web organic result > 0: PASS
- strict five-title matrix: 5/5 PASS
- KEEPWORDS removed from actual Web query: PASS
- packaged EXE Gate A: PASS
- packaged EXE Gate B: PASS
- Desktop localhost Gate C: PASS
- same EXE used for A/B/C: PASS
- A/B/C SHA identity: PASS
- distributed direct EXE SHA == tested EXE SHA: PASS
- downloaded ZIP-contained EXE SHA == tested EXE SHA: PASS
- final EXE SHA differs from old bad Build #107 SHA: PASS
- distribution ZIP obtained and independently re-downloaded: PASS
- distribution EXE physically inspected: PASS
- unresolved issues: 0

STATUS: COMPLETE
