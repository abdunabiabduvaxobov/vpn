# Phase 08 — Deferred Items

Out-of-scope discoveries logged during plan execution (not fixed — they are
unrelated to the task that surfaced them).

## DEF-08-02-A — Pre-existing `TestMigrations019_020` ordering bug

- **Found during:** Plan 08-02, Task 2 (running the full migration test suite after adding migration 027).
- **File:** `server/api/migrations/migrations_test.go` (NOT touched by 08-02).
- **Symptom:** `TestMigrations019_020` fails with `apply 024_admin_panel_overhaul.sql: relation "lava_webhook_events" does not exist (SQLSTATE 42P01)`.
- **Root cause:** The test's apply loop iterates all `*.sql` files in lexicographic order but explicitly **skips** `019/020/021` (it applies them after the loop with per-stage assertions). Migration `024_admin_panel_overhaul.sql` references `lava_webhook_events`, a table created by `020_lava_payments.sql`. Because 020 is skipped in the loop, 024 is applied **before** 020 and fails. This is a test-harness ordering defect that predates 08-02.
- **Independent of 08-02:** The new `027_admin_search_index.sql` sorts after 024 and is never reached; the test does not reference 027. Removing 027 would not change this failure.
- **Why deferred:** SCOPE BOUNDARY — the failing assertion is in a file 08-02 did not modify. Fixing the loop (apply 019/020/021 inline in order, or apply 024+ only after the staged migrations) is a separate change.
- **Suggested owner:** A Phase 08 migration-hardening plan or a quick task; fix is to apply the staged 019/020/021 in numeric order within the loop rather than deferring them past 024.
