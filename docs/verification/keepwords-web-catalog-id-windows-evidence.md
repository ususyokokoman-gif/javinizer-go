# KEEPWORDS / Web Catalog-ID Windows Verification Evidence

STATUS: INCOMPLETE

## Purpose
Prove KEEPWORDS extraction/retention and title-to-Catalog-ID Web resolution in the final Windows executable, not only in source tests.

## Historical defect
- Old bad Build #107 EXE SHA-256: `5e11e68b27b3aa75eccb207a503453c6399c15541084269c4ee1f3db8e433197`
- Build #119 source SHA: `65a4fba83c5a07bd9ed771b1d278139456474eb8`
- Build #119 run: `34784835861`
- Build #119 Windows job: `103798292341`
- First strict packaged-EXE failure: Gate A did not prove that the built EXE received at least one organic Web-search result.
- Build #119 evidence artifact: `10326800300`
- Build #119 evidence ZIP digest: `sha256:c7e1a88e1b19a348f5d3ccc2644a9f273c73b7c622be470ea6ce1d6b2bb71081`

The Build #119 source live contract could obtain DuckDuckGo organic results, but the packaged EXE encountered Google throttling/interstitials and DuckDuckGo pages without parseable organic cards. Direct metadata could still identify SSIS-001, but the strict packaged-EXE gate correctly rejected that as proof of public Web search.

## Remediation
Relevant source history:
- `6dc67c3f8b4611d2c0b0494783d780da280ebb63` — harden packaged title Web-search fallback.
- `6b89b720cdbaef916908ec4ba5587de96f1bd9f1` — avoid KEEPWORD false-positive evidence checks.
- `84469f8243177ab29a6e2b9cb05f2b4ecc640fb` — add Yahoo Japan organic-search fallback.
- `3c64c4f6ad32b598da4ed18c4184b4e2d6bb8d36` — accept Yahoo Japan as strict organic evidence.
- `ef61120ebc2ecb17b579810acd85d8943a30010e` — normalize Bing organic tracking URLs to their real target.
- `5f2df61bdcffcd3ee07f9c06f2c8534db54d8e87` — regression test for Bing redirect catalog extraction.
- `0c261b85e88635ff4580a6844cf7acec2d5b95c0` — make Gate C polling observable on self-hosted Windows.

## Self-hosted Windows evidence
Runner repository: `ususyokokoman-gif/gpt-windows-validation`  
Runner: `SOPI` / labels `self-hosted, windows, x64`

Successful source live diagnostic:
- run: `36157809881`
- job: `108146588120`
- verified source: `a554f4070e10a4265f760f8885e946b6e4e62a56`
- strict provider: `yahoojp`
- strict organic results: `10`
- matrix: `LIVE_MATRIX_PASS=5/5`

Full self-hosted run #10:
- run: `36164637958`
- job: `108169292436`
- source: `5f2df61bdcffcd3ee07f9c06f2c8534db54d8e87`
- deterministic worker E2E: PASS
- regressions: PASS
- live 5-title contract: PASS
- frontend/build: PASS
- Windows EXE build: PASS
- Gate A: PASS
- Gate B: PASS
- Gate C: not accepted; runner job ended while the Gate C step remained reported in-progress and the console log stopped before a normal PowerShell exception/result.
- Distribution: not published from this failed run.

The final self-hosted run is being repeated with Gate C first and periodic poll output, while preserving the same Gate C functional assertions. Final SHA and release evidence will be added only after all gates pass.

## Mandatory final verification
- deterministic tests: PENDING FINAL RECORD
- public Web organic result > 0: PENDING FINAL RECORD
- five-title matrix 5/5: PENDING FINAL RECORD
- KEEPWORDS removed from Web query: PENDING FINAL RECORD
- Gate A: PENDING FINAL RECORD
- Gate B: PENDING FINAL RECORD
- Gate C: PENDING
- same EXE for A/B/C: PENDING
- tested EXE SHA == distributed EXE SHA: PENDING
- new EXE SHA != old bad Build #107 SHA: PENDING
- distribution ZIP/release asset inspected: PENDING
- unresolved issues: Gate C final run and distribution proof
