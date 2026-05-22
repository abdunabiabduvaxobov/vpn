# Phase 1 Smoke Results

**Started:** 2026-05-22T00:10:56Z
**Working-branch HEAD before tag:** 850a32d1a12bc8872a9f68df8cd065e743a28375

## CI Gate (Task 1)

- [x] 8 hotfix commits present (D-02 order verified)
- [x] `go test ./... -race -count=1` PASS
- [x] `go build ./cmd ./cmd/createadmin` PASS

D-02 commit ordering (chronological, `git log --format=%s --reverse | grep '^hotfix(01)'`):

1. hotfix(01): createadmin reads password from stdin + seeds tier=free [HOTFIX-06]
2. hotfix(01): fail-fast aggregate env validator [HOTFIX-08]
3. hotfix(01): scrub 5xx error bodies + X-Request-ID middleware [HOTFIX-04]
4. hotfix(01): AdminRequired re-reads role from DB per request [HOTFIX-02]
5. hotfix(01): atomic Lua INCR+EXPIRE for rate limiter [HOTFIX-03]
6. hotfix(01): transactional refresh-token rotation [HOTFIX-05]
7. hotfix(01): regression test for subscription downgrade (column+scheduler already correct) [HOTFIX-01]
8. hotfix(01): UNIQUE index on sessions.refresh_token_hash + dedupe [HOTFIX-07]

Last lines of `go test ./... -race -count=1` output:

```
?   	vpnapp/server/api/cmd	[no test files]
ok  	vpnapp/server/api/cmd/createadmin	2.393s
?   	vpnapp/server/api/internal/bot	[no test files]
ok  	vpnapp/server/api/internal/cache	10.412s
ok  	vpnapp/server/api/internal/config	1.890s
ok  	vpnapp/server/api/internal/handler	7.319s
ok  	vpnapp/server/api/internal/middleware	5.042s
?   	vpnapp/server/api/internal/model	[no test files]
ok  	vpnapp/server/api/internal/recovery	3.538s
ok  	vpnapp/server/api/internal/repository	3.289s
ok  	vpnapp/server/api/internal/scheduler	4.023s
```

## Staging Smoke Checklist (Tasks 2-11)

| # | Hotfix | Step | Result | Operator | Notes |
|---|--------|------|--------|----------|-------|
| 1 | HOTFIX-08 | env validator fails fast | TODO | | |
| 2 | HOTFIX-04 | 5xx body scrubbed | TODO | | |
| 3 | HOTFIX-04 | X-Request-ID echoed | TODO | | |
| 4 | HOTFIX-02 | admin demotion takes effect | TODO | | |
| 5 | HOTFIX-03 | rate-limit TTL positive | TODO | | |
| 6 | HOTFIX-05 | refresh leaves single session | TODO | | |
| 7 | HOTFIX-01 | scheduler downgrades expired pro | TODO | | |
| 8 | HOTFIX-07 | EXPLAIN shows Index Scan | TODO | | |
| 9 | HOTFIX-06 | createadmin stdin + free tier | TODO | | |
| 10 | regression | existing endpoints still 200/JSON | TODO | | |

## Tag Push (Task 13)

- [ ] `v2.2.0-hotfix` tag created at <commit sha>
- [ ] Tag pushed to remote

---

**Operator approval gate:** Task 13 greps for the literal line `<!-- ALL_SMOKE_STEPS_APPROVED -->`
on its own line. AFTER every row above is recorded as passed AND the Tag Push checkboxes are
ready, append that exact line as the last line of this file (or replace this paragraph with it).
The gate exits 0 only when that literal line is present un-nested.
