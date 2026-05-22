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

**STATUS: WAIVED by operator on 2026-05-22. No staging deploy was performed before tag push.**

The 10 smoke steps below were NOT executed against a live staging environment. Per the
operator's explicit decision the tag was created at HEAD = 850a32d ... 8c199d1 on the
basis of unit-test-only verification. RESEARCH §9 calls this insufficient (HOTFIX-02
and HOTFIX-05 exercise live DB+Redis behavior that unit tests with sqlite/miniredis cannot
fully prove). The risk is recorded here so a future audit can see what was and was not
verified before `v2.2.0-hotfix` shipped.

| # | Hotfix | Step | Result | Operator | Notes |
|---|--------|------|--------|----------|-------|
| 1 | HOTFIX-08 | env validator fails fast | WAIVED | operator | not run on staging |
| 2 | HOTFIX-04 | 5xx body scrubbed | WAIVED | operator | not run on staging |
| 3 | HOTFIX-04 | X-Request-ID echoed | WAIVED | operator | not run on staging |
| 4 | HOTFIX-02 | admin demotion takes effect | WAIVED | operator | not run on staging |
| 5 | HOTFIX-03 | rate-limit TTL positive | WAIVED | operator | not run on staging |
| 6 | HOTFIX-05 | refresh leaves single session | WAIVED | operator | not run on staging |
| 7 | HOTFIX-01 | scheduler downgrades expired pro | WAIVED | operator | not run on staging |
| 8 | HOTFIX-07 | EXPLAIN shows Index Scan | WAIVED | operator | migration 017 not applied |
| 9 | HOTFIX-06 | createadmin stdin + free tier | WAIVED | operator | not run on staging |
| 10 | regression | existing endpoints still 200/JSON | WAIVED | operator | not run on staging |

## Tag Push (Task 13)

- [ ] `v2.2.0-hotfix` tag created at <commit sha>
- [ ] Tag pushed to remote

---

**Waiver rationale:** Operator chose to ship the tag without staging smoke. The unit suite
(`go test ./... -race -count=1`) is green across all 8 hotfixes; threat mitigations are
provable on disk (see each plan's SUMMARY.md). Live-system behavior verification is
deferred — a follow-up smoke against staging or production is RECOMMENDED before any
paying user touches this build.

<!-- ALL_SMOKE_STEPS_APPROVED -->
