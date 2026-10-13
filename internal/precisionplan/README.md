# Precision-first next stage (isolated)

This add-only Go package was developed on MINIPC because the Mac workstation
hosting the newer local JAVINIZER commit was offline. It does **not** replace,
rename, move, or overwrite any user video or any Javinizer job.

## What is implemented

- `Build`: read-only, deterministic selection of **confirmed-only**
  organization candidates, holding any missing, outdated, contradictory,
  English-only, failed, excluded, or unverified records.
- Supplemental detection for **completed-but-no-Japanese-original** and
  **upstream-failed** files, without assuming the completion state is confirmation.
- `Queries`: deduped lookup stages by intact SKU (including edition suffix),
  retaining every originating file path. Opaque IDs are skipped.
- `SearchCandidates`: read-only provider interface, per-call timeout,
  global request limit, and per-provider circuit breakers for 429 and regional
  blocking. It always returns candidates as **review**, never confirmed.

The state `Verified` is only valid when produced by a trusted *server-side*
current-policy decision. Do not accept this field from an API request or
external scraped response.

## Integration into latest Mac repository

Source baseline: GitHub `ususyokokoman-gif/javinizer-go` main
(`58f4510` at development time).
Latest Mac local tree as last verified: branch `local/desktop-noauth`,
commit `8424b9b`, including independent `internal/decision`,
`worker.AuditJobIdentity`, and the shared `StartApply` safety gate.

When Mac access resumes:

1. Inspect actual repository branch, HEAD, status and rules. Preserve the
   existing unrelated dirty auth changes.
2. Cherry-pick or copy this **add-only** package; run its tests.
3. **On the server**, convert the freshly computed
   `worker.AuditJobIdentity` output into `precisionplan.Verdict`.
   Populate `Verified=true` only for items confirmed by a current-policy
   authoritative decision, never from a client.
4. Build the planner's `File` records from the real job snapshot, keyed
   by each exact file path, not a movie ID lookup that picks a first file.
5. Expose only read-only report and query-plan endpoints, initially.
6. Connect LibreDMM/other selected official scraper adapters via
   `SearchCandidates`; enforce provider-level rate limits in the adapter.
   Search results stay **review** until rechecked against authoritative data.
7. Keep the existing shared worker apply gate mandatory. Never turn a
   `Ready` list directly into a rename without final preflight, collision
   detection, change history, and rollback support.
8. Validate on the 498-file persisted batch using a temporary read-only
   copy of SQLite, then repeat on 5,000–7,000 file Shadow Runs.
   Required goals: zero false confirmations and zero file modifications.

This isolated work is unit-tested, but NOT yet integrated or E2E tested
against the newer Mac-local build.
