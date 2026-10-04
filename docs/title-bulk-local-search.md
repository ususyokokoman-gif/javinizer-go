# Title bulk local search

The title-bulk GUI skips duplicate detection. Mac uses eight workers; Windows
uses four. Filename catalog IDs are resolved directly. Otherwise the resolver
searches `title_ja` and `title_en` in the R18.dev SQLite FTS5 trigram index,
retrieves up to five candidates, and verifies a decisive candidate with the
existing Jev gate (default threshold 0.80). Weak, ambiguous, rejected or
unavailable local matches use the existing Web search. Borderline Jev votes
retain their existing behavior.

GUI logs include `LOCAL_TITLE_SEARCH=HIT` for a Jev-accepted local candidate,
`MISS` for no decisive accepted match, and `UNAVAILABLE` for missing/disabled
local lookup or a database failure. Embedded catalog IDs and resumed cache
results do not perform title search.

The normal GUI/CLI run does not download a missing dump. It logs its expected
path and falls back to Web immediately. Prepare the dump before processing:

```sh
JAVINIZER -prepare-title-db
JAVINIZER -prepare-title-db -r18-dump /path/to/r18dev_dump.db
```

Preparation explicitly downloads/imports when missing, or adds the title index
to an existing dump. Default location: the OS user configuration directory plus
`JAVINIZER/r18dev/r18dev_dump.db`. Override with `JAVINIZER_R18DEV_DUMP_PATH`
or CLI `-r18-dump`. Download/preparation errors fail preparation; normal lookup
errors fall back to Web. `-no-local-title-db` uses Web only.

Both Mac and Windows title-bulk builds require CGO and
`-tags desktop,production,sqlite_fts5`. The Mac and Windows title package
workflows include these tags and title/checkpoint regression checks.

Checkpoints still batch at 25 results or two seconds, plus the final save.
State v2 caches are re-resolved once because that version could accept local
matches without Jev; state v3 resumes verified results as before. Older v1
Web-only accepted mappings retain their existing migration behavior.
