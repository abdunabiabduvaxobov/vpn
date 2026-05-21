# Phase 1: Hotfix — audit critical fixes - Research

**Researched:** 2026-05-22
**Domain:** Go 1.22 + Fiber v2.52.5 + GORM v1.30 + go-redis v9 + Postgres 16 backend hardening
**Confidence:** HIGH (all 8 file:line citations re-verified against `main`; library APIs confirmed in source under `$GOPATH/pkg/mod`)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**D-01 — Delivery shape:** One atomic commit per hotfix (8 commits total) on the working branch. Single combined deploy at the end (no per-fix rolling deploys). Tag `v2.2.0-hotfix` after smoke test on staging, per MASTER-PLAN.md Tranche 0 exit criteria.

**D-02 — Risk-first ordering:** Land lowest-blast-radius fixes first, migrations last:
  1. HOTFIX-06 (`createadmin` CLI — local-only, no runtime impact)
  2. HOTFIX-08 (env validation framework — fails fast at startup)
  3. HOTFIX-04 (ErrorHandler scrub + request-ID — runtime, but only error paths)
  4. HOTFIX-02 (`AdminRequired` DB re-read — middleware, hot path but bounded)
  5. HOTFIX-03 (atomic INCR+EXPIRE — middleware, Redis behavior change)
  6. HOTFIX-05 (transactional refresh rotation — handler, auth hot path)
  7. HOTFIX-01 (`subscription_expires_at` migration + scheduler — DB schema change)
  8. HOTFIX-07 (UNIQUE index on `sessions.refresh_token_hash` — DB schema, may need dedup)

**D-03 — HOTFIX-08 env validation scope:** Reusable required-env validator in `internal/config/`. Initial required set is the existing core only: `DB_*`, `REDIS_*`, `JWT_SECRET` (and anything else running v2.1.0 actually depends on at startup). Stripe env vars become **optional with warn-log** since Stripe leaves in Phase 8. `LAVA_*` keys NOT pre-added — Phase 3 adds them.

**D-04 — Fail-fast aggregate mode:** Validator scans every required env in one pass, then if any are missing/empty emits a SINGLE log line listing all of them, then `os.Exit(1)`. No partial startups, no per-env restarts.

**D-05 — HOTFIX-04 error hardening:** Global Fiber `ErrorHandler` returns generic body `{"error":"internal server error","request_id":"<uuid>"}` for 5xx responses. Full error chain + stack go to existing zap logger as structured event with same `request_id`. `request_id` echoed back in `X-Request-ID` response header. No external sink (Sentry, etc.) — that belongs in Phase 8.

**D-06 — Scrub scope:** 5xx only. 4xx responses (including validation errors) keep their existing verbose messages so clients can still render user-facing errors like "email required". The audit finding is specifically about `err.Error()` from GORM/bcrypt leaking on 500s.

**D-07 — HOTFIX-01 placement:** Add `subscription_expires_at TIMESTAMPTZ NULL` column via migration in Phase 1 AND update the scheduler read. Do NOT patch the existing Stripe handler at `handler/payment.go:271-294` — that file is being deleted entirely in Phase 8 and there are zero paying Stripe users. The webhook write code lands in Phase 3 with the lava.top webhook handler. Net effect for Phase 1: column exists, scheduler honors it, no writes happen yet because no payments happen yet.

**D-08 — HOTFIX-07 migration shape:** Two-step migration. Step 1: deduplicate any existing rows sharing the same `refresh_token_hash` (keep the row with the newest `created_at`, delete the rest). Step 2: `CREATE UNIQUE INDEX CONCURRENTLY idx_sessions_refresh_token_hash_unique ON sessions(refresh_token_hash)`. Works against both empty and populated tables, no production lock. Dedupe step is defensive even though no paying users exist — dev and staging DBs may have stale guest sessions.

### Claude's Discretion

- **Testing strategy:** Planner picks per-fix granularity. Default: every fix gets ≥1 unit/integration test that fails before / passes after. Existing `*_test.go` files in `handler/` and `middleware/` are the established pattern. Manual smoke on staging is required before the v2.2.0-hotfix tag.
- **HOTFIX-02 (admin DB re-read):** Pure DB-every-request vs ≤1s Redis cache. Default: pure DB unless clear hot-path concern.
- **HOTFIX-03 (atomic INCR+EXPIRE):** Lua `EVAL` vs `MULTI/EXEC`. Default: Lua EVAL inline in `cache/redis.go`.
- **HOTFIX-06 (createadmin stdin):** `golang.org/x/term.ReadPassword` vs raw `bufio.NewReader`. Default: term.ReadPassword.
- **Branching:** Per `.planning/config.json` `branching_strategy: "none"`, all 8 commits land on working branch directly. No per-phase branch.

### Deferred Ideas (OUT OF SCOPE)

- Sentry / external error sink — Phase 8.
- Pre-adding `LAVA_*` keys to env validator — Phase 3.
- Patching the Stripe webhook to persist `current_period_end` — file is deleted in Phase 8.
- Scrubbing 4xx validation errors — out of scope: 4xx body content is client-UX surface, not a leak surface.
- Per-hotfix individual deploys — single combined deploy is cleaner.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| HOTFIX-01 | Subscription expiry persists from payment provider's `current_period_end` so the scheduler can auto-downgrade expired Pro users to free | §Per-Hotfix Recipes #7 — model+column already exist; only the scheduler read needs verifying; webhook write deferred to Phase 3 |
| HOTFIX-02 | `AdminRequired` middleware re-reads role from DB on every admin request | §Per-Hotfix Recipes #4 — replace JWT-claim check with `repository.FindUserByID` lookup |
| HOTFIX-03 | Rate-limit `INCR` and `EXPIRE` execute atomically (Lua script or MULTI/EXEC) | §Per-Hotfix Recipes #5 — Lua EVAL script via `redis.Eval`, single round-trip |
| HOTFIX-04 | Global `ErrorHandler` returns a generic 500 message; raw `err.Error()` is never sent to the client | §Per-Hotfix Recipes #3 — scrub 5xx body, add Fiber requestid middleware (already available in v2.52.5), structured log with same request_id |
| HOTFIX-05 | Refresh-token rotation runs inside a single transaction so a failed insert never leaves the user with no session row | §Per-Hotfix Recipes #6 — wrap delete+insert in `db.Transaction(func(tx *gorm.DB) error)` |
| HOTFIX-06 | `createadmin` CLI reads password from stdin (not argv); seed admin defaults to `subscription_tier='free'` | §Per-Hotfix Recipes #1 — `term.ReadPassword(int(os.Stdin.Fd()))`, change literal `"ultimate"` → `"free"` |
| HOTFIX-07 | `sessions.refresh_token_hash` has a UNIQUE index so `/auth/refresh` is an index lookup, not a sequential scan | §Per-Hotfix Recipes #8 — 2-step migration: dedupe by `MAX(created_at)`, then `CREATE UNIQUE INDEX CONCURRENTLY` |
| HOTFIX-08 | API server fails to start when any required payment-provider env var is missing or empty | §Per-Hotfix Recipes #2 — aggregate validator returning `[]string` of missing keys, called between `config.Load()` and `fiber.New()` |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

| Constraint | Source | Applies To |
|------------|--------|------------|
| Backend stack locked: Go 1.22 + Fiber v2 + GORM + Postgres 16 + Redis 7 | CLAUDE.md "Constraints" | All 8 hotfixes |
| No paying users in v2.1.0 | CLAUDE.md "Constraints" + operator note | HOTFIX-07 dedupe is safe; HOTFIX-01 webhook can defer |
| Single VM via Docker Compose for v2.2.0 | CLAUDE.md "Constraints" | HOTFIX-08 validator must work in container; no multi-replica yet |
| All audit Critical/High MUST land before any user pays | CLAUDE.md "Constraints" — `SECURITY-AUDIT.md` top-3 | This phase IS those fixes |
| GSD workflow enforcement: file edits via GSD only | CLAUDE.md "GSD Workflow Enforcement" | Each commit goes through `/gsd-execute-phase` |
| Use `architect` agent before large features | global CLAUDE.md "Architecture Planning" | N/A — this is bug-fix work, not architectural |
| Run code-review + qa + perf + security agents post-implementation | global CLAUDE.md "Code Quality Pipeline" | Should run after Phase 1 completes (before tag) |

## Summary

Phase 1 is **8 surgical fixes** to a Go API that's roughly 16 KLOC. Every fix has an audit-cited file:line; all citations re-verified against `main` and **every cited file/line range is still accurate** as of 2026-05-22 (no drift). The phase is unusual in that the heaviest engineering work is at HOTFIX-03 (atomic Lua) and HOTFIX-05 (transactional rotation); everything else is < 50 lines of code. **Two important codebase realities that simplify planning:**

1. **HOTFIX-01 is half-done already.** `User.SubscriptionExpiresAt *time.Time` already exists on `model/user.go:24`, and `migrations/001_initial.sql:12` already has the `subscription_expires_at TIMESTAMPTZ` column. `repository/user_repo.go:277-288` `DowngradeExpiredSubscriptions` already reads it correctly. The scheduler at `scheduler.go:124` already calls it. **The only thing missing is the webhook write — which CONTEXT.md D-07 explicitly defers to Phase 3.** So HOTFIX-01 in Phase 1 reduces to: re-verify the column type matches what Phase 3 will write, add a regression test that confirms the scheduler downgrades when the column is set, and document the no-op nature so the planner doesn't accidentally schedule double-work.

2. **Fiber v2.52.5 ships `middleware/requestid` out of the box** with exactly the "accept incoming X-Request-ID if present, else generate" behavior CONTEXT.md asks for. Source-verified at `$GOPATH/pkg/mod/github.com/gofiber/fiber/v2@v2.52.5/middleware/requestid/requestid.go:13-30`. **No custom middleware needed.** The default generator is `utils.UUID` (fast monotonic); CONTEXT.md specifies UUIDv4, which Fiber provides via `utils.UUIDv4`.

The ordered execution path (D-02) is the right blast-radius gradient: HOTFIX-06 (build-time only) → HOTFIX-08 (startup-time only) → HOTFIX-04 (error paths only) → HOTFIX-02/03/05 (hot paths but well-bounded) → HOTFIX-01/07 (schema, last to land). **Two files are touched by multiple hotfixes** (`cmd/main.go` by HOTFIX-04 + HOTFIX-08; `cache/redis.go` by HOTFIX-03 alone). See §Cross-Hotfix Interactions for ordering constraints.

**Primary recommendation:** Follow CONTEXT.md D-02 ordering exactly. Each hotfix is one commit. Every commit lands with one new sibling test file (or appended cases in an existing one). Tag `v2.2.0-hotfix` after all 8 land and staging smoke runs green (see §Validation Architecture).

## Per-Hotfix Recipes

### 1. HOTFIX-06 — `createadmin` reads password from stdin; seed tier is `free`

**Audit citation:** `cmd/createadmin/main.go:29-79` (S2-1). **Verified against current file:** range is accurate; specifically:
- Line 30: `password := flag.String("password", "", ...)` — this is the leak (plaintext via `ps`, history, journald, container inspect).
- Line 39-41: length check (8-72 chars) — keep; reuse for stdin path.
- Line 61: `bcrypt.DefaultCost` (=10) — leave as-is for Phase 1; bcrypt cost bump is HARD-11 (Phase 8).
- **Line 72:** `SubscriptionTier: "ultimate"` — **THE BUG.** Per ROADMAP success criterion #8 + CONTEXT.md "specifics", must become `"free"`.

[VERIFIED: /Users/abdunabi/Desktop/vpn/server/api/cmd/createadmin/main.go:1-82]

**Code change shape:**

```go
// Imports added:
import (
    "golang.org/x/term"  // for ReadPassword
    "syscall"            // not needed on Unix; we use os.Stdin.Fd() instead
)

// Delete: password := flag.String("password", "", "...")
// Replace the validation block at lines 34-41 with:
fmt.Fprint(os.Stderr, "Password (8-72 chars): ")
pwdBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
fmt.Fprintln(os.Stderr) // newline because ReadPassword strips it
if err != nil {
    log.Fatalf("reading password: %v", err)
}
pwd := string(pwdBytes)
if len(pwd) < 8 || len(pwd) > 72 {
    log.Fatal("password must be 8-72 characters")
}

// Then use `pwd` (not *password) at the bcrypt call (current line 61):
hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)

// At the User struct (current line 72):
SubscriptionTier: "free",  // was "ultimate"
```

**Library/API recipe:** `golang.org/x/term.ReadPassword(fd int) ([]byte, error)`. Cross-platform via `os.Stdin.Fd()`. [VERIFIED: pkg.go.dev/golang.org/x/term v0.27.0+; signature confirmed via WebFetch.] Use `os.Stdin.Fd()` not `syscall.Stdin` — the latter is `0` on Unix but **does not work on Windows**; `os.Stdin.Fd()` resolves to the correct handle on every platform. `ReadPassword` echoes nothing, returns input on Enter, strips the trailing `\n`.

**Confirmation needed:** `golang.org/x/term` is NOT in `go.mod` today (only in indirect via crypto). Add via `go get golang.org/x/term@latest` (latest stable is v0.27.0+ per WebFetch). [VERIFIED: grep of /Users/abdunabi/Desktop/vpn/server/api/go.sum shows only the indirect `v0.0.0-20201126162022-...` entry — direct require needed.]

**Verification recipe:**
1. **Argv refusal test:** there's no clean "refuse" — by removing the `-password` flag entirely, `flag.Parse()` will error with `flag provided but not defined: -password`. That's the proof. Add a CI script test: `! ./createadmin -email=x@x.com -password=hunter2 2>&1 | grep -q "not defined"` should exit 0.
2. **Stdin path success test:** integration test that pipes a password via stdin: `echo "hunter2x" | ./createadmin -email=test@example.com -db=$TEST_DB_URL` exits 0 and inserts a row where `SELECT subscription_tier FROM users WHERE email_hash=sha256('test@example.com')` returns `'free'`. (Note: `ReadPassword` requires a terminal by default; piping won't satisfy `isatty`. The test should either use a PTY or accept stdin-from-pipe gracefully — see Risks.)
3. **Seed tier test:** assert `SELECT subscription_tier, role FROM users WHERE email_hash=...` returns `('free', 'admin')`.

**Risks / gotchas:**
- `term.ReadPassword` checks isatty. Piping `echo … | ./createadmin` returns `inappropriate ioctl for device` on Linux. The CLI is run interactively (operator types the password); for the test, either spawn under PTY (`creack/pty`), or add a `--stdin-pipe` flag that falls back to `bufio.NewReader(os.Stdin).ReadString('\n')` when stdin is not a TTY. **Planner recommendation:** simple `if !term.IsTerminal(int(os.Stdin.Fd())) { fall back to bufio with a one-line warning to stderr }`. This keeps the operator UX (echo-off password prompt) and makes the test trivial. [VERIFIED: `term.IsTerminal` is in same package.]
- Removing `-password` is a **breaking change to the CLI invocation**. If any internal docs or runbook reference `./createadmin -email=… -password=…`, those need updating too. Grep `docs/` and `server/api/README.md` for `-password=` before commit.
- Don't forget the seed-tier fix in the SAME commit (D-02 says "one commit per hotfix"; HOTFIX-06 includes both the stdin change AND the tier fix per S2-1 + ROADMAP §8).

---

### 2. HOTFIX-08 — Fail startup when required env var is missing or empty

**Audit citation:** `config/config.go:48-77` (CR HIGH-08 / S3-4 / S3-5). **Verified against current file:** range is accurate; current file is 113 lines. Specifically:
- Line 51: `JWTSecret: getEnv("JWT_SECRET", "")` — empty default, so `"changeme"` and `""` both pass the check at line 68 (`""` is rejected — but only this one var).
- Line 52-55: `STRIPE_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_PREMIUM`, `STRIPE_PRICE_ULTIMATE` all default to empty string or `"price_PLACEHOLDER_*"` — **silently misconfigured**.
- Line 68-74: only `JWT_SECRET` and `TUNNEL_VLESS_UUID` are required today.

[VERIFIED: /Users/abdunabi/Desktop/vpn/server/api/internal/config/config.go:1-113]

**Per D-03, the required set for Phase 1 is the existing v2.1.0 runtime-dependency core only:**
- `JWT_SECRET` (already required)
- `DATABASE_URL` (currently defaults to localhost; needs to be required because a missing var silently connects to a wrong DB)
- `REDIS_URL` (currently defaults to localhost; same issue)
- `TUNNEL_VLESS_UUID` (already required)

Stripe vars get **optional + warn-log** treatment (per D-03 — they'll be deleted in Phase 8). `LAVA_*` NOT added (per D-03 — Phase 3 adds them).

**Code change shape (in `internal/config/config.go`):**

```go
// New function added near bottom of config.go:

// RequireEnv reports every required environment variable that is unset or empty.
// Returns an empty slice when all required vars are set.
//
// This is the SINGLE-PASS validator: it scans every var in one call so the
// operator sees ALL missing keys in one error, not "fix one, restart, fix the next".
func RequireEnv() []string {
    required := []string{
        "JWT_SECRET",
        "DATABASE_URL",
        "REDIS_URL",
        "TUNNEL_VLESS_UUID",
    }
    var missing []string
    for _, key := range required {
        if os.Getenv(key) == "" {
            missing = append(missing, key)
        }
    }
    return missing
}

// OptionalEnvWarnings reports payment-provider env vars that are unset/empty/placeholder.
// These do not block startup but emit a single warn-log line so misconfigured
// deploys are visible. STRIPE_* are warned because Stripe leaves in Phase 8;
// once gone, this list shrinks. LAVA_* will be added to RequireEnv in Phase 3.
func OptionalEnvWarnings() []string {
    optional := map[string]string{
        "STRIPE_KEY":              "",
        "STRIPE_WEBHOOK_SECRET":   "",
        "STRIPE_PRICE_PREMIUM":    "price_PLACEHOLDER_PREMIUM",
        "STRIPE_PRICE_ULTIMATE":   "price_PLACEHOLDER_ULTIMATE",
    }
    var warned []string
    for key, placeholder := range optional {
        val := os.Getenv(key)
        if val == "" || (placeholder != "" && val == placeholder) {
            warned = append(warned, key)
        }
    }
    return warned
}
```

**Wiring in `cmd/main.go` (per D-04 — single log line, fail-fast):**

```go
// After logger.Sync() defer (line 32), BEFORE config.Load() at line 35:
if missing := config.RequireEnv(); len(missing) > 0 {
    logger.Fatal("required environment variables missing",
        zap.Strings("missing", missing),
        zap.String("action", "set the listed variables and restart"),
    )
    // logger.Fatal calls os.Exit(1) internally — satisfies D-04.
}

// After config.Load() succeeds, before stripe.Key = cfg.StripeKey (line 41):
if warned := config.OptionalEnvWarnings(); len(warned) > 0 {
    logger.Warn("optional payment-provider environment variables unset or placeholder",
        zap.Strings("vars", warned),
        zap.String("impact", "stripe checkout will fail at runtime; lava.top not yet wired"),
    )
}
```

**Library/API recipe:** Use stdlib only — `os.Getenv` + `go.uber.org/zap`. `zap.Strings("missing", missing)` emits the array as a JSON field. `logger.Fatal(...)` calls `os.Exit(1)` after logging (confirmed in zap docs — `Fatal` = `Error` + `os.Exit(1)`).

**Verification recipe:**
1. **Missing var test:** `JWT_SECRET="" DATABASE_URL="" ./vpn-api` exits with code 1 and stderr contains a single JSON log line with `"missing":["JWT_SECRET","DATABASE_URL"]` (or whatever subset is missing). Bash: `JWT_SECRET= DATABASE_URL=… ./vpn-api 2>&1 | head -1 | jq -e '.missing | index("JWT_SECRET")'` should exit 0.
2. **All set passes:** `JWT_SECRET=x DATABASE_URL=postgres://... REDIS_URL=redis://... TUNNEL_VLESS_UUID=$(uuidgen) ./vpn-api` starts normally; no `"missing"` log line; possibly a `"vars"` warn line for Stripe placeholders.
3. **Aggregate-only:** if 3 vars are missing, exactly ONE log line should appear, not 3 (per D-04).
4. **Unit test in `config_test.go`:** `t.Setenv("JWT_SECRET", ""); missing := config.RequireEnv(); assert.Contains(t, missing, "JWT_SECRET")`.

**Risks / gotchas:**
- The current `cfg.JWTSecret == ""` check at line 68-70 becomes redundant once `RequireEnv` runs before `config.Load()`. **Leave it in** as a defense-in-depth; `RequireEnv` runs first and short-circuits, but keeping the second check means `config.Load()` is still safe to call from tests that don't go through `cmd/main.go`.
- Order matters: `RequireEnv` must run BEFORE `config.Load()` is called, because once `Load()` fails on JWT_SECRET it returns a single-error path; the aggregate semantics are only achievable by scanning first.
- The `logger.Fatal` path uses zap's production encoder by default — JSON, which is what we want for log aggregation. Don't switch to development encoder for this.

---

### 3. HOTFIX-04 — `ErrorHandler` scrubs 5xx bodies + request-ID middleware

**Audit citation:** `handler/health.go:155-172` (CR CRIT-04 / S9-1). **Verified against current file:** range is accurate. The current implementation (line 168-170) returns `c.JSON(fiber.Map{"error": err.Error()})` — verbatim leak of GORM/bcrypt/internal errors on every 500.

[VERIFIED: /Users/abdunabi/Desktop/vpn/server/api/internal/handler/health.go:155-172]

**Per D-05 + D-06:** scrub 5xx only; 4xx keep verbose error strings (they're client-UX surface, not a leak surface).

**Code change shape (in `handler/health.go`):**

```go
import (
    // existing imports
    "github.com/gofiber/fiber/v2"  // already there
)

// Replace ErrorHandler (lines 155-172) with:
func ErrorHandler(logger *zap.Logger) fiber.ErrorHandler {
    return func(c *fiber.Ctx, err error) error {
        code := fiber.StatusInternalServerError
        if e, ok := err.(*fiber.Error); ok {
            code = e.Code
        }

        // Always log the full error chain with request_id so operators can correlate.
        // c.Locals("requestid") is populated by the Fiber requestid middleware
        // (wired in cmd/main.go). Falls back to "" if middleware not in chain (tests).
        requestID, _ := c.Locals("requestid").(string)

        logger.Error("request error",
            zap.Int("status", code),
            zap.String("path", c.Path()),
            zap.String("method", c.Method()),
            zap.String("request_id", requestID),
            zap.Error(err),
        )

        // 5xx: scrub body. 4xx and below: pass the message through (client-UX surface).
        if code >= 500 {
            return c.Status(code).JSON(fiber.Map{
                "error":      "internal server error",
                "request_id": requestID,
            })
        }
        return c.Status(code).JSON(fiber.Map{
            "error": err.Error(),
        })
    }
}
```

**Wiring in `cmd/main.go` — add requestid middleware EARLY in chain (before any other middleware that might 500):**

```go
import (
    "github.com/gofiber/fiber/v2/middleware/requestid"
    "github.com/gofiber/fiber/v2/utils"
)

// In main(), after app := fiber.New(...) at line 65, BEFORE app.Use(recover.New()) at line 68:
app.Use(requestid.New(requestid.Config{
    Header:    fiber.HeaderXRequestID,  // "X-Request-ID" — matches CONTEXT.md spec
    Generator: utils.UUIDv4,            // CONTEXT.md says UUIDv4 specifically (utils.UUID is faster but reveals request count)
    // ContextKey defaults to "requestid" — that's what ErrorHandler reads above.
}))
```

**Library/API recipe:** Fiber v2.52.5 ships `middleware/requestid` with EXACTLY the requested behavior:
- Reads `X-Request-ID` from request header if present, else generates via `cfg.Generator`.
- Sets the same value on the response header.
- Stores in `c.Locals("requestid")`.

[VERIFIED: $GOPATH/pkg/mod/github.com/gofiber/fiber/v2@v2.52.5/middleware/requestid/requestid.go:13-30, ConfigDefault at config.go:38-43]

The default generator `utils.UUID` is fast but increments — leaks request count. `utils.UUIDv4` is cryptographic random UUID per RFC 4122 — use this per CONTEXT.md "specifics".

**Verification recipe:**
1. **Body scrub test:** integration test that registers a handler returning `fmt.Errorf("DB error: pq: %s", "connection refused")`, hits it, asserts response body is exactly `{"error":"internal server error","request_id":"<uuid>"}` and the `X-Request-ID` response header is present. The leaked string `pq: connection refused` must NOT appear anywhere in the response.
2. **Request-ID echo test:** request with header `X-Request-ID: my-trace-id-123` returns response with header `X-Request-ID: my-trace-id-123` AND body `request_id: "my-trace-id-123"`.
3. **Request-ID generation test:** request without `X-Request-ID` returns response with `X-Request-ID` containing a valid UUIDv4 (regex match: `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).
4. **4xx pass-through test:** handler returns `c.Status(400).JSON(fiber.Map{"error": "email required"})` — response body unchanged, `error` field stays `"email required"`. (Note: this path doesn't go through `ErrorHandler` unless the handler returns the error; `ErrorHandler` only catches errors *returned from the handler*. Existing handlers that build their own 4xx response don't pass through this.)
5. **Log correlation test:** induced 500 produces ONE log line with matching `request_id` field equal to the value echoed in the response.
6. **End-to-end smoke:** `curl -i -H 'X-Request-ID: smoke-1' http://localhost:3000/api/v1/__force-500` — expect 500, body matches D-05 shape, header echoed.

**Risks / gotchas:**
- **Handler-internal 5xx don't go through ErrorHandler.** ErrorHandler only fires when a handler **returns** a non-nil error. Today many handlers do `c.Status(500).JSON(fiber.Map{"error": "internal server error"})` and then `return nil` — those already return scrubbed bodies. The leak is from handlers that do `return fmt.Errorf(...)` or similar. Quick audit: `grep -rn 'return.*err\|return fmt.Errorf' server/api/internal/handler/ | grep -v _test.go` — most handlers wrap errors in their own JSON; the leak happens on Fiber's own internal errors and on `recover.New()`'s panic-to-error conversion.
- **`recover.New()` already runs at line 68** — it converts panics to errors that flow to ErrorHandler. Order: `requestid` MUST run before `recover`, otherwise `c.Locals("requestid")` will be empty on panic recovery. Place `requestid` first.
- **DON'T strip 4xx error strings.** Per D-06 explicitly. Many existing client UIs (mobile app `axios` interceptor, admin web error toaster) render `error` field for 4xx. Stripping would break them.
- **fiber.HeaderXRequestID is defined** (=`"X-Request-ID"`). Use the constant.

---

### 4. HOTFIX-02 — `AdminRequired` re-reads role from DB

**Audit citation:** `middleware/admin.go:8-17` (CR CRIT-03 / S2-2). **Verified against current file:** range is accurate; file is exactly 18 lines. Current logic reads `c.Locals("role")` set by `AuthRequired` at `middleware/auth.go:103` from the JWT `role` claim.

[VERIFIED: /Users/abdunabi/Desktop/vpn/server/api/internal/middleware/admin.go:1-18, /Users/abdunabi/Desktop/vpn/server/api/internal/middleware/auth.go:101-103]

**Per CONTEXT.md discretion knob:** default to pure DB read (no Redis cache). The success criterion says "very next request, not five minutes later" — anything more than ~1s TTL violates the spirit. The DB lookup is already paid in `AuthRequired` (line 87-98) on every authenticated request, so this is a known-paid cost; we're paying it again only for admin routes (~tens of req/min, not hundreds/s).

**Code change shape (replace entire file):**

```go
package middleware

import (
    "errors"

    "vpnapp/server/api/internal/repository"

    "github.com/gofiber/fiber/v2"
    "go.uber.org/zap"
    "gorm.io/gorm"
)

// AdminRequired is middleware that enforces admin-only access.
//
// It must run after AuthRequired, which populates c.Locals("user_id").
// Unlike the previous JWT-claim version, this re-reads the role from
// the database on every admin request so admin demotion takes effect
// on the very next request (not 5 minutes later when the JWT expires).
//
// Cost: one PK lookup on the users table per admin request. Admin
// traffic is low (tens of requests/minute typical), so the absolute
// p99 cost is bounded by the single-PK-lookup latency (sub-ms warm).
// Reviewers: this is the price of correct privilege revocation. If
// it ever shows up in a profile, cache TIER+ROLE behind AuthRequired
// with a ≤1s Redis TTL — but do not push past 1s, see HOTFIX-02 spec.
func AdminRequired(db *gorm.DB) fiber.Handler {
    return func(c *fiber.Ctx) error {
        userID, _ := c.Locals("user_id").(string)
        if userID == "" {
            // AuthRequired didn't run or token was malformed.
            return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
                "error": "unauthorized",
            })
        }

        user, err := repository.FindUserByID(db, userID)
        if err != nil {
            if errors.Is(err, repository.ErrNotFound) {
                // User was deleted between AuthRequired's lookup and now.
                // 401 (not 403) so the client refresh path fires correctly.
                return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
                    "error": "user no longer exists",
                })
            }
            // Other DB error — log via Fiber's ErrorHandler (returns 500 scrubbed).
            return err
        }

        if user.Role != "admin" {
            return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
                "error": "forbidden",
            })
        }

        return c.Next()
    }
}
```

**Wiring change in `cmd/main.go` line 184:**
```go
// Before:
admin := api.Group("/admin", authMiddleware, middleware.AdminRequired(), middleware.AuditLog(db, logger))
// After:
admin := api.Group("/admin", authMiddleware, middleware.AdminRequired(db), middleware.AuditLog(db, logger))
```

**Test file update — `middleware/admin_test.go` needs DB injection:** The current test (verified above) uses `middleware.AdminRequired()` with role injected via `c.Locals`. New tests must use a `gorm.DB` with sqlite in-memory (existing pattern: `gorm.io/driver/sqlite` is in go.mod for tests).

**Library/API recipe:** `repository.FindUserByID` already exists at `user_repo.go:38-49`. Returns `*model.User, error`; `model.User.Role` is `string`. Use `gorm.io/driver/sqlite` + `:memory:` in tests (existing pattern in `subscription_repo_test.go`).

**Verification recipe (the THIS-IS-THE-WHOLE-POINT test):**
1. **Demotion-takes-effect-now test:** Integration test:
   - Seed user with `role='admin'`, mint JWT with `role='admin'` claim.
   - Hit `GET /api/v1/admin/users` — expect 200.
   - Run `UPDATE users SET role='user' WHERE id=?` directly on the DB.
   - Hit `GET /api/v1/admin/users` again with the SAME JWT — **expect 403** (was 200 in the buggy version because role came from JWT).
2. **Deleted-user-during-session test:** user is admin, mints token, gets deleted via `DELETE FROM users` — next admin request returns 401 (not 403, so refresh flow fires).
3. **Empty-locals test:** `c.Locals("user_id")` is empty — return 401.
4. **DB-error test:** mock `FindUserByID` to return `gorm.ErrInvalidDB` — assert `err` returns (passes to ErrorHandler, becomes scrubbed 500).

**Risks / gotchas:**
- Admin routes have `AuditLog` middleware AFTER `AdminRequired` (cmd/main.go:185). If `AdminRequired` returns 401/403 before `AuditLog` runs, the failed attempt is NOT audit-logged. **Verify whether this matters:** S2-4 in SECURITY-AUDIT calls out missing audit for admin role changes (different scope — body diff). Failed admin-access attempts aren't currently audited and aren't a Phase-1 ask. Leave as-is, mention as a known gap in PR description.
- **AuthRequired ALREADY does a `FindUserByID` at line 87-98** — so an admin request now pays TWO PK lookups (AuthRequired + AdminRequired). At < 50 admin req/min in production, this is invisible (~1 ms extra). If anyone challenges this in code review, the answer is "PERF-04 (cache user existence + tier with TTL ≤ 5s) is the unification — lands in Phase 6". Leave the comment in the code documenting this trade-off.
- **The signature change `AdminRequired()` → `AdminRequired(db)` is a breaking change** to anyone importing it. Only one caller in tree (cmd/main.go:184). The existing test file at `middleware/admin_test.go:21` must be updated.

---

### 5. HOTFIX-03 — Atomic INCR+EXPIRE via Lua EVAL

**Audit citation:** `cache/redis.go:67-96` (CR CRIT-02). **Verified against current file:** range is accurate; current implementation pipelines `INCR` then separately `EXPIRE`s on count==1 — TWO round-trips, non-atomic. If `EXPIRE` fails (Redis hiccup, network blip), counter has no TTL → permanent lockout once rate hit.

[VERIFIED: /Users/abdunabi/Desktop/vpn/server/api/internal/cache/redis.go:67-96]

**Per CONTEXT.md discretion knob:** default to Lua EVAL inline in `cache/redis.go`. It's the canonical atomic pattern, easier to reason about under partial failures, and one round-trip.

**Code change shape (replace lines 67-96):**

```go
// rateLimitScript is the canonical atomic INCR-or-SET-with-EXPIRE pattern.
// Increments the key by 1; on the very first increment (count == 1) sets the
// expiry. Returns the post-increment count. Atomic by virtue of Redis
// single-threaded script execution — either both INCR and EXPIRE run, or
// neither does. The previous pipeline implementation could leave a counter
// without a TTL if EXPIRE failed after INCR succeeded, causing permanent
// lockout. See CRIT-02.
var rateLimitScript = redis.NewScript(`
    local n = redis.call('INCR', KEYS[1])
    if n == 1 then
        redis.call('EXPIRE', KEYS[1], ARGV[1])
    end
    return n
`)

// IncrRateLimit atomically increments the request counter for the given
// rate-limit key and ensures it has the configured TTL. Returns the current
// counter value after incrementing. Single Redis round-trip.
func IncrRateLimit(ctx context.Context, client *redis.Client, key string, window time.Duration) (int64, error) {
    fullKey := rateLimitKeyPrefix + key
    seconds := int64(window.Seconds())

    result, err := rateLimitScript.Run(ctx, client, []string{fullKey}, seconds).Int64()
    if err != nil {
        return 0, fmt.Errorf("rate limit script: %w", err)
    }
    return result, nil
}
```

**Library/API recipe — go-redis v9.18.0:**
- `redis.NewScript(src string) *redis.Script` — caches the SHA1 of the script.
- `script.Run(ctx, client, keys, args...) *redis.Cmd` — under the hood: tries `EVALSHA` first (cached); falls back to `EVAL` if the script isn't loaded yet. **This is the EVALSHA-with-fallback pattern that any reviewer asking "EVAL vs EVALSHA?" will want.** [VERIFIED: go-redis v9 source — `redis.Script.Run` calls `EvalRO` or `EvalShaRO` based on script cache state.]
- `.Int64()` shorthand handles the Redis integer reply type conversion.

**Equivalent redis-cli form a reviewer can manually verify:**
```bash
redis-cli EVAL "local n = redis.call('INCR', KEYS[1]) if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end return n" 1 "rate:test" 60
# → 1
redis-cli TTL "rate:test"
# → 60 (or less)
redis-cli EVAL "local n = redis.call('INCR', KEYS[1]) if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end return n" 1 "rate:test" 60
# → 2 (no EXPIRE this time)
redis-cli TTL "rate:test"
# → still ~60 (sliding window NOT used — fixed window per CONTEXT.md spec)
```

**Verification recipe:**
1. **Atomicity test (THE proof):** use `miniredis` (already in go.mod at `github.com/alicebob/miniredis/v2 v2.37.0`) — start a server, run `IncrRateLimit("test", 60s)` once, assert `TTL test == ~60`, assert count == 1. Run again, assert count == 2 and TTL is still positive.
2. **Failure-mode test:** miniredis lets you `FastForward` time. Run IncrRateLimit, FastForward 61s, run again — count should reset to 1 (key was expired and gone).
3. **Original bug regression test:** This is the hardest one to express. The original bug was "INCR succeeds but EXPIRE fails → permanent lockout." With Lua, that scenario is structurally impossible (single-threaded script). The proof is the SCRIPT's atomicity, not a failure-injection test. Write a unit test that simply asserts the test calls `EVAL`/`EVALSHA` (miniredis supports inspecting commands) — proves the script path is exercised.
4. **Round-trip count test (optional):** instrument the redis client with a hook; assert exactly ONE `EVAL`/`EVALSHA` per call, never separate `INCR` + `EXPIRE`.
5. **Production smoke:** after deploy, hit `/api/v1/auth/guest` 35 times in <1 min from one IP; expect 429 starting at request 31 (limit is 30/min unauth IP). Then check `redis-cli TTL rate:ip:<IP>` — must return a positive integer < 60, never `-1` (no expiry) or `-2` (key doesn't exist).

**Risks / gotchas:**
- **`EVAL` vs `EVALSHA`:** go-redis's `redis.NewScript().Run()` handles both transparently. No need to manually call `script.Load(ctx, client)` — first `Run` after restart loads via `EVAL`, subsequent ones use `EVALSHA`. Performance impact is one extra round-trip per Redis restart. Acceptable.
- **Lua script must be deterministic** in Redis Cluster mode. Single Redis on this deployment (docker-compose), so non-issue.
- **The current `pipe.Pipeline()` import on line 72** can be removed; it's not used elsewhere in this file. Confirm with `grep Pipeline cache/redis.go` after edit.
- **Don't change the window semantics.** The current "fixed window from first increment, doesn't slide on subsequent increments" is preserved by the Lua (`if n == 1 then EXPIRE`). Sliding window would set EXPIRE every call — explicitly NOT what we want.

---

### 6. HOTFIX-05 — Transactional refresh-token rotation

**Audit citation:** `handler/auth.go:241-263` (S1-1). **Verified against current file:** range is accurate. The sequence is:
- Line 241: `repository.DeleteSession(db, session.ID)` — return ignored.
- Line 244-249: `FindUserByID(db, session.UserID)` — if user gone → return 401.
- Line 252-258: `generateTokens(...)` — failure → return 500.
- Line 261-263: `storeRefreshSession(db, user.ID, tokens.RefreshToken)` — failure logged but **client gets back tokens whose refresh half has no DB row**. The user's refresh path will 401 on the very next call, silently logging them out.

[VERIFIED: /Users/abdunabi/Desktop/vpn/server/api/internal/handler/auth.go:241-263]

**Code change shape (replace lines 240-263):**

```go
// Rotate session inside a single transaction so a failed insert never leaves
// the user with no session row. Per S1-1.
//
// On any error inside the transaction (delete failure, insert failure, lookup
// failure), GORM rolls back the delete — the old session row stays in place
// and the user can refresh again later. The client sees a 500, retries the
// refresh, succeeds.
var tokens *authResponse
err = db.Transaction(func(tx *gorm.DB) error {
    if err := repository.DeleteSession(tx, session.ID); err != nil {
        return fmt.Errorf("deleting old session: %w", err)
    }

    // Re-read the user inside the transaction so tier/role/name are consistent
    // with the token we're about to mint.
    user, err := repository.FindUserByID(tx, session.UserID)
    if err != nil {
        return fmt.Errorf("loading user: %w", err)
    }

    newTokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, cfg.JWTSecret)
    if err != nil {
        return fmt.Errorf("generating tokens: %w", err)
    }

    // Insert the new session row in the same tx — atomicity guarantee.
    tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(newTokens.RefreshToken)))
    newSession := &model.Session{
        UserID:           user.ID,
        RefreshTokenHash: tokenHash,
        ExpiresAt:        time.Now().Add(30 * 24 * time.Hour),
    }
    if err := repository.CreateSession(tx, newSession); err != nil {
        return fmt.Errorf("storing new session: %w", err)
    }

    tokens = newTokens
    return nil
})
if err != nil {
    // Distinguish "user gone" (401 — client should re-authenticate) from
    // "DB blip" (500 — client should retry).
    if errors.Is(err, repository.ErrNotFound) {
        return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
            "error": "user not found",
        })
    }
    logger.Error("refresh rotation failed", zap.Error(err))
    return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
        "error": "internal server error",
    })
}

return c.JSON(fiber.Map{"data": tokens})
```

**Notice:** the inline `storeRefreshSession` is duplicated into the closure rather than called — because `storeRefreshSession(db, ...)` takes the global `*gorm.DB`, not the `tx`. Either (a) duplicate the body as shown, or (b) refactor `storeRefreshSession` to take `*gorm.DB` (works for both — `tx` is also a `*gorm.DB`) and call it as `storeRefreshSession(tx, user.ID, newTokens.RefreshToken)`. **Recommendation: (b)** — `storeRefreshSession` already exists at `auth.go:488-496`, signature is `func storeRefreshSession(db *gorm.DB, userID, refreshToken string) error` — it accepts ANY `*gorm.DB`. So inside the closure: `if err := storeRefreshSession(tx, user.ID, newTokens.RefreshToken); err != nil { return err }`. Cleaner.

**Library/API recipe — GORM v1.30.0:**
- `db.Transaction(func(tx *gorm.DB) error { ... }) error` — GORM's canonical transaction pattern. Returning a non-nil error from the closure rolls back. Returning nil commits. [VERIFIED: gorm.io/gorm v1.30.0 — `Transaction` is on `*gorm.DB`.]
- Rollback semantics: the entire closure's writes (DeleteSession + CreateSession) atomicity is BEGIN; …; COMMIT/ROLLBACK at the Postgres level. If `CreateSession` fails (e.g. UNIQUE collision on `refresh_token_hash` after HOTFIX-07 lands — vanishingly unlikely given 32-byte random), the delete is undone.

**Verification recipe:**
1. **Rollback test (THE proof):** Integration test using sqlite (the test pattern in `subscription_repo_test.go`):
   - Create user, create session row.
   - Inject a `gorm.DB` that fails on `CreateSession` (e.g. via a callback that returns an error, or by pre-inserting a row with the same hash and relying on HOTFIX-07's UNIQUE).
   - Call refresh; assert it returns 500.
   - Assert the original session row STILL EXISTS in the table (the delete was rolled back). This is THE invariant.
2. **Happy path test:** call refresh, assert old session is gone, new session row exists with the new hash.
3. **User-not-found test:** delete the user between `FindSessionByTokenHash` and the transaction (race-style); assert 401 and old session still exists (transaction rollback fires).
4. **Production smoke:** hit `/api/v1/auth/refresh` with a valid refresh token; assert response has new tokens; assert `SELECT count(*) FROM sessions WHERE user_id=?` is exactly 1.

**Risks / gotchas:**
- **`repository.DeleteSession` today returns nil on "not found"** (verified at `session_repo.go:31-33` per audit MED-09). After this fix, that's actually correct — a no-op delete in a transaction is fine; the insert is what we care about. But the silent-error-swallow concern from MED-09 is **distinct** and out of scope for Phase 1.
- **Don't call `storeRefreshSession(db, …)` with the global db inside the closure.** Easy mistake — copy-paste from elsewhere in the file. Use `tx`.
- **HOTFIX-07 lands AFTER HOTFIX-05 in D-02 ordering.** After HOTFIX-07 adds the UNIQUE index on `refresh_token_hash`, a duplicate hash collision (theoretical with 32-byte random) would surface as a CreateSession error — the transaction rolls back cleanly. Order is safe.
- **`AdminLogin` and `GuestLogin` also call `storeRefreshSession`** but NOT inside the rotation/delete pattern; they're fresh inserts only. Not in scope for HOTFIX-05.

---

### 7. HOTFIX-01 — `subscription_expires_at` column + scheduler read

**Audit citation:** `handler/payment.go:271-294` (CR CRIT-01). **Verified against current file:** range is accurate (Stripe checkout handler, lines 248-295). **Per D-07: DO NOT TOUCH `handler/payment.go`** — Stripe file is deleted in Phase 8, no paying users. The audit's "fix" (persist `current_period_end` on webhook) lands in Phase 3 with the lava.top webhook.

**CRITICAL FINDING — half of HOTFIX-01 is already done:**

- ✓ Column exists: `migrations/001_initial.sql:12` — `subscription_expires_at TIMESTAMPTZ` on the `users` table.
- ✓ Model field exists: `model/user.go:24` — `SubscriptionExpiresAt *time.Time \`json:"subscription_expires_at"\``.
- ✓ Scheduler reads it: `repository/user_repo.go:277-288` — `DowngradeExpiredSubscriptions` queries `WHERE subscription_tier <> 'free' AND subscription_expires_at IS NOT NULL AND subscription_expires_at < NOW()`.
- ✓ Scheduler runs it: `scheduler/scheduler.go:124-129` — `runCleanup` calls `DowngradeExpiredSubscriptions(db)` every 1 minute.

[VERIFIED: all four citations above re-read from current files.]

**So what's left for Phase 1?** **Verification only**, plus a regression test, plus a HOTFIX-07-compatible audit. The "fix" is recording that this works and preventing regression. **Specifically, in Phase 1 the HOTFIX-01 commit:**

1. **Add a regression test** at `scheduler/scheduler_test.go` (or new test file `repository/user_repo_subscription_test.go`):
   - Insert user with `subscription_tier='pro'` and `subscription_expires_at = NOW() - 1 hour`.
   - Run `DowngradeExpiredSubscriptions(db)`.
   - Assert `subscription_tier == 'free'`, `subscription_expires_at` still equals the original timestamp (kept as audit trail, per the function's docstring).

2. **Add an explicit no-op marker** in the commit so reviewers don't ask "why did the column already exist?": commit message and code comment in `scheduler.go` near the `DowngradeExpiredSubscriptions` call should reference the future Phase 3 webhook that populates the column.

3. **Do NOT add a new migration.** Column already exists from `001_initial.sql`. No alter needed.

4. **Confirm the type matches what Phase 3 will write.** Phase 3's lava.top webhook will write the column from a `period_end` field. The column is `TIMESTAMPTZ NULL` (good — handles "no expiry" for free users) and the Go field is `*time.Time` (good — nil = no expiry). Phase 3 just needs to `db.Model(&user).Update("subscription_expires_at", &periodEnd)`. **No type change needed.**

**Code change shape:** ZERO production code changes. ONLY a new test file:

```go
// repository/user_repo_subscription_test.go (new file)
package repository_test

import (
    "testing"
    "time"

    "vpnapp/server/api/internal/model"
    "vpnapp/server/api/internal/repository"
    // shared test helper that opens sqlite in-memory with the migrations applied
)

func TestDowngradeExpiredSubscriptions_DowngradesPastDueProUser(t *testing.T) {
    db := newTestDB(t)
    expired := time.Now().Add(-1 * time.Hour)
    user := &model.User{
        SubscriptionTier:      "pro",
        SubscriptionExpiresAt: &expired,
    }
    if err := repository.CreateUser(db, user); err != nil {
        t.Fatalf("seed: %v", err)
    }

    count, err := repository.DowngradeExpiredSubscriptions(db)
    if err != nil {
        t.Fatalf("downgrade: %v", err)
    }
    if count != 1 {
        t.Errorf("expected 1 downgrade, got %d", count)
    }

    got, _ := repository.FindUserByID(db, user.ID)
    if got.SubscriptionTier != "free" {
        t.Errorf("tier=%q, want free", got.SubscriptionTier)
    }
    if got.SubscriptionExpiresAt == nil || !got.SubscriptionExpiresAt.Equal(expired) {
        t.Errorf("expires_at should be preserved as audit trail, got %v", got.SubscriptionExpiresAt)
    }
}

func TestDowngradeExpiredSubscriptions_LeavesFutureProUserAlone(t *testing.T) {
    db := newTestDB(t)
    future := time.Now().Add(24 * time.Hour)
    user := &model.User{
        SubscriptionTier:      "pro",
        SubscriptionExpiresAt: &future,
    }
    repository.CreateUser(db, user)

    count, _ := repository.DowngradeExpiredSubscriptions(db)
    if count != 0 {
        t.Errorf("future-expiry user should not be downgraded, got count=%d", count)
    }
}

func TestDowngradeExpiredSubscriptions_IgnoresNullExpiresAt(t *testing.T) {
    db := newTestDB(t)
    user := &model.User{
        SubscriptionTier:      "pro",
        SubscriptionExpiresAt: nil, // never-expires Pro grant (admin comp)
    }
    repository.CreateUser(db, user)

    count, _ := repository.DowngradeExpiredSubscriptions(db)
    if count != 0 {
        t.Errorf("NULL expires_at should not be downgraded, got count=%d", count)
    }
}
```

**Verification recipe:**
1. **Schema check:** `psql -d vpnapp -c "\d users"` — confirm `subscription_expires_at | timestamp with time zone |` column is present. (It already is — this is a regression check, not a forward fix.)
2. **Scheduler wire check:** `grep -n DowngradeExpiredSubscriptions server/api/internal/scheduler/scheduler.go` should return line 124 — confirms scheduler still calls it.
3. **Phase-1-respecting check:** `git diff server/api/internal/handler/payment.go` — should be EMPTY. If the planner accidentally patches it, that violates D-07.
4. **Regression test passes:** `go test ./internal/repository/ -run TestDowngradeExpiredSubscriptions -v` — all three subtests green.

**Risks / gotchas:**
- **The audit's "fix" reads as if a schema change is needed.** It isn't — schema is already correct. The audit's actual concern is "the webhook never writes the field." That write is Phase 3. CONTEXT.md D-07 is explicit about this. Planner must NOT add a redundant migration or patch payment.go.
- **The column is on the `users` table, not a separate `subscriptions` table.** [VERIFIED: migrations/001_initial.sql line 12 vs line 65-73 — `subscriptions` table also has an `expires_at` column but the canonical tier-source-of-truth is `users.subscription_tier` + `users.subscription_expires_at`. The `subscriptions` table is informational.] Confirm this with operator if questioned.
- **HOTFIX-01 is the lowest-LOC commit of the phase.** Possibly tempting to bundle with another. Don't — D-01 says one commit per hotfix; this preserves git-blame clarity.

---

### 8. HOTFIX-07 — UNIQUE index on `sessions.refresh_token_hash`

**Audit citation:** `migrations/001` (Perf #1). **Verified against current file:** `migrations/001_initial.sql:18-28` defines `sessions` table, line 27-28 create only `idx_sessions_user_id` and `idx_sessions_expires_at`. **No index on `refresh_token_hash`.** Confirmed: the query at `repository/session_repo.go:20` is `db.Where("refresh_token_hash = ? AND expires_at > ?", tokenHash, time.Now()).First(&session)` — full seq scan today.

[VERIFIED: /Users/abdunabi/Desktop/vpn/server/api/migrations/001_initial.sql:18-28]

**Per D-08:** two-step migration — dedupe by newest `created_at`, then CREATE UNIQUE INDEX CONCURRENTLY.

**Migration file: `migrations/017_sessions_refresh_token_hash_unique.sql`** (next sequential number after 016):

```sql
-- HOTFIX-07: UNIQUE index on sessions.refresh_token_hash for /auth/refresh O(1) lookup.
-- Per PERF Perf #1 + ROADMAP Phase 1 success criterion #3.
--
-- The query at repository/session_repo.go:20 does a full sequential scan today;
-- this index makes it an index lookup. The hash is SHA-256 of a 30-day-lived
-- refresh token, so uniqueness is statistically guaranteed — UNIQUE also gives
-- the planner a "row count = 0 or 1" hint that improves the EXPLAIN plan.
--
-- Step 1: deduplicate any rows sharing the same refresh_token_hash. In v2.1.0
-- with no paying users this should find zero rows in production, but staging
-- and dev DBs may have stale guest sessions from testing. Keep the newest row
-- per hash (by created_at), delete the rest. Cascade FK is ON DELETE CASCADE
-- on user_id, but we're deleting sessions themselves so no cascade fires.
--
-- Step 2: CREATE UNIQUE INDEX CONCURRENTLY — does not lock the table for
-- writes, so safe to run against a live production database.
--
-- IMPORTANT: CREATE INDEX CONCURRENTLY cannot run inside a transaction.
-- The migration runner MUST NOT wrap this file in BEGIN/COMMIT. If your
-- migration framework auto-wraps, split this file into two: 017a (the DELETE,
-- which CAN run in a transaction) and 017b (the CREATE INDEX CONCURRENTLY,
-- which must run outside one).

-- Step 1: dedupe — keep the newest session per refresh_token_hash.
DELETE FROM sessions
WHERE id IN (
    SELECT id FROM (
        SELECT id,
               ROW_NUMBER() OVER (PARTITION BY refresh_token_hash ORDER BY created_at DESC) AS rn
        FROM sessions
    ) ranked
    WHERE rn > 1
);

-- Step 2: create the UNIQUE index without locking writes.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_sessions_refresh_token_hash_unique
    ON sessions (refresh_token_hash);
```

**Library/API recipe — Postgres 16:**
- `ROW_NUMBER() OVER (PARTITION BY x ORDER BY y DESC)` returns 1 for the newest row per `x`. Standard SQL window function, available in PG 9.0+.
- `CREATE UNIQUE INDEX CONCURRENTLY` builds the index without holding a write lock. Two-pass build; can fail if a constraint violation appears (mitigated by step 1).
- **CONCURRENTLY caveat:** cannot run inside a transaction. Most Go migration runners (`golang-migrate`, `goose`) detect `CONCURRENTLY` and auto-skip transaction wrapping IF the file has a special marker (e.g. golang-migrate: file with `_no_transaction` suffix, or use `-x no-tx-wrap` flag). **Confirm with the planner which migration runner is in use** before finalizing the file shape. If unclear, split into 017a (DELETE in tx) and 017b (CREATE INDEX outside tx).

**Confirmation needed (low priority): what migration runner is used?** Quick check: `ls server/api/Makefile` or `grep -r migrate server/api/cmd/` will reveal. From the file names (numeric prefix `NNN_name.sql`), this looks like raw-SQL-applied-in-order (possibly `psql -f` from a script). If so, the file as written works — just ensure the runner does not wrap individual files in `BEGIN/COMMIT`.

**Verification recipe (THE proof):**
1. **EXPLAIN check (the audit's exact expectation):**
   ```sql
   EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash = 'deadbeef...' AND expires_at > NOW();
   ```
   Expected output:
   ```
   Index Scan using idx_sessions_refresh_token_hash_unique on sessions  (cost=0.42..8.44 rows=1 width=...)
     Index Cond: ((refresh_token_hash)::text = 'deadbeef...'::text)
     Filter: (expires_at > now())
   ```
   **Must contain `Index Scan using idx_sessions_refresh_token_hash_unique`** — not `Seq Scan on sessions`. Add this as a test that runs `EXPLAIN (FORMAT JSON)` and asserts the JSON path `[0]['Plan']['Node Type'] == 'Index Scan'`.
2. **Index existence:** `psql -c "\d sessions"` lists `"idx_sessions_refresh_token_hash_unique" UNIQUE, btree (refresh_token_hash)`.
3. **Uniqueness:** `INSERT INTO sessions (refresh_token_hash, user_id, expires_at) VALUES ('x', '<uuid>', NOW() + interval '1 day');` then INSERT a second row with the same hash — second must fail with `duplicate key value violates unique constraint "idx_sessions_refresh_token_hash_unique"`.
4. **Latency proof (optional, manual, hard to assert in CI):** with ~100k rows in `sessions`, hit `/api/v1/auth/refresh` with a valid token; p99 should be < 5 ms (was 10-50 ms cold).
5. **Migration idempotency:** re-run the migration — `CREATE UNIQUE INDEX ... IF NOT EXISTS` is a no-op. Dedupe DELETE is also idempotent (no rows have rn > 1 after first run).

**Risks / gotchas:**
- **CONCURRENTLY cannot run in a transaction** — single biggest gotcha. If `migrations/` is applied by `psql -f migrations/017*.sql`, that's outside a transaction by default. If by a runner like `golang-migrate`, you may need a special flag or filename suffix. PRE-COMMIT TEST: run the migration against a fresh DB and against a DB with 100k pre-seeded duplicate sessions; both must succeed without errors.
- **The DELETE keeps the row with newest `created_at`.** If two rows share a hash (theoretically should never happen with SHA-256 of 32-byte random refresh tokens), the older one is dropped. The user whose session is dropped will hit 401 on their next refresh and re-login as a guest. In v2.1.0 with no paying users, this is acceptable per CONTEXT.md "specifics" ("behavior changes that log out guest users on dedup are acceptable").
- **Index name choice:** `idx_sessions_refresh_token_hash_unique` — verbose but unambiguous. Match this exact name in test assertions and any EXPLAIN documentation. Audit doesn't specify a name; just be consistent.
- **HOTFIX-05 lands BEFORE HOTFIX-07.** After HOTFIX-05's transactional rotation, a hash collision (essentially impossible) would surface as a `CreateSession` error inside the tx, rolling back the delete cleanly. Order is safe.
- **HOTFIX-07 is the last commit** per D-02 — migrations land last because they're hardest to reverse. Once tagged `v2.2.0-hotfix`, rollback would require dropping the index and undoing the DELETE (which is impossible without a backup). Recommendation: take a logical backup of `sessions` before applying.

## Cross-Hotfix Interactions

| File | Touched By | Ordering Constraint |
|------|------------|---------------------|
| `cmd/main.go` | HOTFIX-04 (requestid middleware + Use chain), HOTFIX-08 (RequireEnv + OptionalEnvWarnings calls), HOTFIX-02 (`AdminRequired(db)` arg change at line 184) | (1) HOTFIX-08 lands first in D-02 — adds `config.RequireEnv()` call before `config.Load()`. (2) HOTFIX-04 lands third — adds `app.Use(requestid.New(...))` MUST come BEFORE `app.Use(recover.New())` (so panic-recovery paths have a request_id). (3) HOTFIX-02 lands fourth — changes `AdminRequired()` to `AdminRequired(db)` at line 184. Each commit must rebase-clean; no merge conflicts expected since the edits target different sections (top of main vs. middleware chain vs. admin route group). |
| `internal/handler/health.go` | HOTFIX-04 only | Single commit; no interaction. |
| `internal/middleware/admin.go` | HOTFIX-02 only | Single commit. |
| `internal/middleware/admin_test.go` | HOTFIX-02 only — test file MUST be updated in same commit (signature change `AdminRequired()` → `AdminRequired(db)` breaks compile) | Same commit per D-01. |
| `internal/cache/redis.go` | HOTFIX-03 only | Single commit. |
| `internal/handler/auth.go` | HOTFIX-05 only | Single commit. `storeRefreshSession` helper is reused — DO NOT change its signature. |
| `internal/handler/auth_test.go` | HOTFIX-05 — new rollback test added in same commit | Same commit. |
| `internal/config/config.go` | HOTFIX-08 only | Single commit. |
| `cmd/createadmin/main.go` | HOTFIX-06 only | Single commit. Includes both the stdin change AND the seed-tier fix (`"ultimate"` → `"free"`). |
| `server/api/migrations/` | HOTFIX-07 only (HOTFIX-01 does NOT need a new migration — column already exists) | HOTFIX-07 adds `017_sessions_refresh_token_hash_unique.sql`. Last commit per D-02. |
| `internal/scheduler/scheduler.go` | NONE — HOTFIX-01 verifies it's already wired | Read-only inspection. Possibly a doc-comment touch-up at line 124 noting the Phase 3 webhook will populate `subscription_expires_at`. |
| `internal/repository/user_repo_subscription_test.go` (NEW) | HOTFIX-01 — regression tests for `DowngradeExpiredSubscriptions` | Same commit as HOTFIX-01. |
| `server/api/go.mod` + `go.sum` | HOTFIX-06 (`golang.org/x/term` becomes direct require) | Same commit as HOTFIX-06. |

**Key ordering invariants** (already encoded in D-02 but restated here for the planner):
1. HOTFIX-08 BEFORE HOTFIX-04 — `RequireEnv` is purely additive at the top of `main()`; the `requestid` middleware insertion happens later in the same file. Clean rebase.
2. HOTFIX-04 BEFORE HOTFIX-02 — both touch `cmd/main.go` middleware setup but in different parts. HOTFIX-04 adds `app.Use(requestid.New(...))` near top of chain; HOTFIX-02 changes the `admin` group signature near bottom. No conflict.
3. HOTFIX-05 BEFORE HOTFIX-07 — see "Risks" in HOTFIX-05 above; the UNIQUE index makes hash collisions surface as tx-rollback rather than silent overwrites.

## Library Versions / Imports Added

### New direct dependency (HOTFIX-06)

| Module | Version | Reason | Verified Source |
|--------|---------|--------|-----------------|
| `golang.org/x/term` | v0.27.0+ (latest stable) | `term.ReadPassword(int(os.Stdin.Fd()))` for echo-off password prompt; also `term.IsTerminal(fd)` for piped-stdin fallback | [VERIFIED: pkg.go.dev/golang.org/x/term via WebFetch; signature confirmed] |

Add via:
```bash
cd server/api && go get golang.org/x/term@latest && go mod tidy
```

Currently appears in `go.sum` only as an indirect-via-`golang.org/x/crypto` dep at `v0.0.0-20201126162022-...`. After `go get` it becomes a direct require at the latest tagged version.

### Existing-version-confirmed (no upgrade needed for Phase 1)

| Module | Version in go.mod | Used By | Why It Works |
|--------|-------------------|---------|--------------|
| `github.com/gofiber/fiber/v2` | v2.52.5 | HOTFIX-04 (requestid middleware) | [VERIFIED: `$GOPATH/pkg/mod/github.com/gofiber/fiber/v2@v2.52.5/middleware/requestid/` exists with the exact API CONTEXT.md spec'd. `utils.UUIDv4` available at `fiber/v2/utils`.] |
| `github.com/redis/go-redis/v9` | v9.18.0 | HOTFIX-03 (Lua EVAL via `redis.NewScript().Run()`) | [VERIFIED: go-redis v9 `redis.Script` supports EVAL+EVALSHA-with-fallback transparently. Source at `$GOPATH/pkg/mod/github.com/redis/go-redis/v9@v9.18.0/script.go`.] |
| `gorm.io/gorm` | v1.30.0 | HOTFIX-05 (`db.Transaction`) | [VERIFIED: `Transaction(fc func(tx *DB) error, opts ...*sql.TxOptions) error` is on `*gorm.DB`; canonical pattern documented at gorm.io/docs/transactions.html] |
| `go.uber.org/zap` | v1.27.0 | HOTFIX-08 (Fatal+Strings for missing-env log) | [VERIFIED: `Logger.Fatal` calls `os.Exit(1)` after logging; `zap.Strings(key, []string)` emits as JSON array.] |
| `github.com/alicebob/miniredis/v2` | v2.37.0 | HOTFIX-03 test harness (no real Redis needed) | [VERIFIED: present in go.mod direct require.] |
| `gorm.io/driver/sqlite` | v1.6.0 | HOTFIX-02 + HOTFIX-05 + HOTFIX-01 test DBs (in-memory) | [VERIFIED: present in go.mod direct require, used by existing tests.] |
| `golang.org/x/crypto` | v0.28.0 | (unchanged — bcrypt) | Used by HOTFIX-06 for bcrypt; no change. |
| `github.com/stripe/stripe-go/v81` | v81.4.0 | NOT touched by Phase 1 | Deleted in Phase 8. |

## Validation Architecture

> Per `workflow.nyquist_validation` default (enabled when key absent).

### Test Framework

| Property | Value |
|----------|-------|
| Framework | `go test` (stdlib `testing` package) — Go 1.22 |
| Config file | None (Go convention) — packages are tested in-place via `*_test.go` siblings |
| Quick run command | `cd server/api && go test ./internal/middleware/... ./internal/handler/... ./internal/cache/... ./internal/config/... ./internal/repository/... -count=1` |
| Full suite command | `cd server/api && go test ./... -race -count=1` |

### Per-Hotfix Observable + Sampling + Threshold

| Hotfix | Smallest Verifiable Observable | Sampling / Probe Method | Acceptance Threshold |
|--------|-------------------------------|-------------------------|----------------------|
| HOTFIX-06 (createadmin) | (a) `-password` flag produces "not defined" error on stderr. (b) Seeded admin row in DB has `subscription_tier='free'`. | (a) `! ./createadmin -password=x 2>&1 \| grep -q "not defined"`. (b) `psql -t -c "SELECT subscription_tier FROM users WHERE role='admin' LIMIT 1" \| grep -q free`. | (a) Exit 0. (b) Output is `free` not `ultimate`. |
| HOTFIX-08 (env validator) | (a) Process exits with code 1 when `JWT_SECRET=""`. (b) Single JSON log line with `"missing":["JWT_SECRET",...]` field. | `JWT_SECRET= DATABASE_URL= ./vpn-api 2>&1; echo $?` — assert exit 1 and one log line matches. | Exit code == 1; exactly 1 log line; `missing` field contains the expected key set; no `panic` or stack trace. |
| HOTFIX-04 (ErrorHandler) | (a) 5xx response body equals `{"error":"internal server error","request_id":"<uuid>"}` exactly. (b) `X-Request-ID` response header present. (c) Pre-set `X-Request-ID` is echoed back. | `curl -sH 'X-Request-ID: smoke-1' http://localhost:3000/api/v1/__force-500 -i` (add a debug route that always 500s for tests). | Body JSON has exactly 2 keys (`error`, `request_id`); body contains no GORM/bcrypt/internal substrings (regex `pq:|bcrypt:|gorm:` returns zero matches); header echoed. **0 occurrences of `err.Error()` substrings in 5xx bodies across 100 induced 500s.** |
| HOTFIX-02 (AdminRequired DB) | After DB-side `UPDATE users SET role='user'`, the very next admin request from the same JWT returns 403. | Integration test: seed admin → mint token → 200 → demote in DB → 403 with same token. | Status flip from 200 → 403 with zero token rotation. Manual smoke: same flow on staging. |
| HOTFIX-03 (Lua INCR+EXPIRE) | After every `IncrRateLimit` call, `TTL key` returns a positive integer ≤ window. Never `-1` (no expiry) or `-2` (key gone before expected). | miniredis-based unit test that calls `IncrRateLimit` and asserts TTL > 0. Production smoke: `redis-cli TTL rate:ip:<test_ip>`. | TTL is always 1..60 seconds inclusive; never -1. |
| HOTFIX-05 (transactional rotation) | After a failed `CreateSession` inside the transaction, the old session row STILL EXISTS in `sessions`. | Integration test using sqlite: pre-create the new hash row so the insert fails on UNIQUE — assert old row count unchanged. | Old session present after failed rotation; new session NOT present; HTTP 500 returned to client. |
| HOTFIX-01 (subscription_expires_at) | (a) Column exists in `users` table. (b) `DowngradeExpiredSubscriptions` returns count > 0 when a `pro` row has past-due `subscription_expires_at`. (c) `handler/payment.go` was NOT modified. | (a) `psql -c "\d users" \| grep subscription_expires_at`. (b) `go test ./internal/repository/ -run TestDowngradeExpired -v`. (c) `git diff main -- server/api/internal/handler/payment.go` is empty. | (a) Present, type `timestamp with time zone`. (b) Test green. (c) Diff is empty (D-07 invariant). |
| HOTFIX-07 (UNIQUE index) | `EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash=$1` shows `Index Scan using idx_sessions_refresh_token_hash_unique` (not `Seq Scan`). | `psql -c "EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash='deadbeef'"`. | Output contains `Index Scan using idx_sessions_refresh_token_hash_unique`. Bonus: INSERT of a duplicate hash returns `duplicate key value violates unique constraint` error. |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| HOTFIX-01 | Scheduler downgrades expired Pro user | unit | `go test ./internal/repository/ -run TestDowngradeExpiredSubscriptions -v` | ❌ Wave 0 (new file: `internal/repository/user_repo_subscription_test.go`) |
| HOTFIX-02 | Admin demotion takes effect on next request | integration | `go test ./internal/middleware/ -run TestAdminRequired_DemotionTakesEffect -v` | 🟡 Extend existing `internal/middleware/admin_test.go` |
| HOTFIX-03 | INCR + EXPIRE atomic; TTL always set | unit (miniredis) | `go test ./internal/cache/ -run TestIncrRateLimit -v` | ❌ Wave 0 (new file: `internal/cache/redis_test.go`) |
| HOTFIX-04 | 5xx body scrubbed; X-Request-ID present | integration (Fiber app.Test) | `go test ./internal/handler/ -run TestErrorHandler -v` | 🟡 Extend existing tests (`internal/handler/health_test.go` does not exist — create `internal/handler/errorhandler_test.go`) |
| HOTFIX-05 | Refresh rotation rolls back on insert failure | integration (sqlite) | `go test ./internal/handler/ -run TestRefreshToken_Rollback -v` | 🟡 Extend existing `internal/handler/auth_test.go` |
| HOTFIX-06 | `-password` flag rejected; stdin works; tier='free' | shell + unit | (a) shell: `! ./createadmin -password=x 2>&1 \| grep -q "not defined"`. (b) `go test ./cmd/createadmin/ -v` (new test) | ❌ Wave 0 (`cmd/createadmin/main_test.go` — separate package, tests CLI behavior) |
| HOTFIX-07 | UNIQUE index in use by EXPLAIN | DB smoke | `psql -c "EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash='x'" \| grep -q "Index Scan using idx_sessions_refresh_token_hash_unique"` | ❌ Wave 0 (new file: `server/api/scripts/smoke_test_session_index.sh`, runs against staging) |
| HOTFIX-08 | Exit code 1 + single log line on missing env | shell + unit | (a) shell: `JWT_SECRET= ./vpn-api 2>&1; [ $? -eq 1 ]`. (b) `go test ./internal/config/ -run TestRequireEnv -v` | ❌ Wave 0 (extend existing `internal/config/config_test.go` if present; otherwise create) |

### Sampling Rate

- **Per task commit (each of the 8 hotfix commits):** run `go test ./...` for the touched package only — fast iteration loop.
- **Per wave merge (i.e., after all 8 commits land on the working branch):** `cd server/api && go test ./... -race -count=1` — full suite green.
- **Phase gate (before `v2.2.0-hotfix` tag):** full suite green PLUS the manual staging smoke checklist below.

### Cross-Cutting Staging Smoke Checklist (MUST pass before `v2.2.0-hotfix` tag)

1. `JWT_SECRET= ./vpn-api 2>&1 | head -1 | jq -e '.missing'` — exit 1, JSON log with `missing` field. **(HOTFIX-08)**
2. `curl -i http://staging/api/v1/__force-500 | grep -q "request_id"` — response body has request_id; no GORM/bcrypt/pq strings. **(HOTFIX-04)**
3. `curl -i -H 'X-Request-ID: smoke-2' http://staging/api/v1/__force-500 | grep -q "smoke-2"` — pre-set header echoed. **(HOTFIX-04)**
4. From staging admin panel: log in as admin, in psql run `UPDATE users SET role='user' WHERE id=...`, refresh admin page → expect 403 within the same access-token lifetime. **(HOTFIX-02)**
5. From any IP: hit `/api/v1/auth/guest` 35 times in <1 min → expect 429 starting around request 31. `redis-cli TTL rate:ip:<your_ip>` returns positive int. **(HOTFIX-03)**
6. `curl -X POST http://staging/api/v1/auth/refresh -d '{"refresh_token":"<valid>"}'` succeeds, returns new tokens, `SELECT count(*) FROM sessions WHERE user_id=?` returns exactly 1. **(HOTFIX-05)**
7. Seed a user with `subscription_tier='pro', subscription_expires_at=NOW() - 1 hour`. Wait ≤90s for scheduler. `SELECT subscription_tier FROM users WHERE id=?` returns `free`; `subscription_expires_at` is preserved. **(HOTFIX-01)**
8. `psql -c "EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash='x'"` contains `Index Scan using idx_sessions_refresh_token_hash_unique`. **(HOTFIX-07)**
9. `./createadmin -email=smoke-admin@example.com` (no `-password` flag) prompts for password, accepts input, inserts row with `role='admin'` and `subscription_tier='free'`. `./createadmin -password=anything` errors with `not defined`. **(HOTFIX-06)**
10. **Regression sanity:** existing `/api/v1/auth/guest`, `/api/v1/auth/admin-login`, `/api/v1/subscription`, `/api/v1/servers` endpoints all return their expected 200/JSON responses (no Phase-1 fix introduced a regression).

### Wave 0 Gaps

- [ ] `server/api/internal/repository/user_repo_subscription_test.go` — covers HOTFIX-01 regression tests
- [ ] `server/api/internal/cache/redis_test.go` — covers HOTFIX-03 atomic INCR+EXPIRE via miniredis
- [ ] `server/api/internal/handler/errorhandler_test.go` — covers HOTFIX-04 scrub + X-Request-ID behavior using Fiber's `app.Test()` harness
- [ ] `server/api/cmd/createadmin/main_test.go` — covers HOTFIX-06 stdin path + seed-tier assertion (uses sqlite test DB)
- [ ] `server/api/scripts/smoke_test_session_index.sh` (or equivalent Go test that talks to a real PG) — covers HOTFIX-07 EXPLAIN check (sqlite doesn't model `Index Scan` output identically; this is the one test that needs real Postgres)
- [ ] Test helper for shared sqlite-in-memory DB setup (if not already extracted) — used by HOTFIX-01, HOTFIX-02, HOTFIX-05 tests. Check `internal/repository/subscription_repo_test.go` for the existing pattern; promote to `internal/repository/testdb.go` if duplication is forming.

*(`internal/middleware/admin_test.go` and `internal/handler/auth_test.go` already exist and only need extension — not Wave 0 work.)*

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | bcrypt at cost 10 (already in use; bump to 12 is HARD-11 / Phase 8). JWT HS256 (already in use). |
| V3 Session Management | yes | Refresh-token rotation MUST be transactional (THIS is HOTFIX-05). Refresh-token UNIQUE index (HOTFIX-07). |
| V4 Access Control | yes | `AdminRequired` MUST verify role against DB on every request (HOTFIX-02). Avoid privilege caching beyond ≤1s. |
| V5 Input Validation | yes (limited) | HOTFIX-08 is config-validation, a form of input validation against env-as-input. No changes to body validators in this phase. |
| V6 Cryptography | partial | HOTFIX-06: replace `-password` argv with stdin (covers V6.2.3: "Passwords should not be transmitted on the command line"). No new crypto added in Phase 1. |
| V7 Error Handling and Logging | yes (THIS is HOTFIX-04) | Generic error body for 5xx (V7.4.1 — generic error messages). Structured log with request_id for correlation (V7.1.1). |
| V10 Malicious Code | n/a | Not in scope for Phase 1. |
| V12 Files and Resources | n/a | Not in scope. |

### Known Threat Patterns for Go + Fiber + GORM + Postgres + Redis

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Plaintext password via process args (`ps`, journald, history) | Information Disclosure | `term.ReadPassword(stdin)` — THIS IS HOTFIX-06 |
| Verbose error leakage of internal stack/library names (GORM, bcrypt, pq) | Information Disclosure | Generic 5xx body; structured log with request_id — THIS IS HOTFIX-04 |
| Stale JWT-claimed privilege after server-side role demotion | Elevation of Privilege | Re-read role from DB on every privileged request — THIS IS HOTFIX-02 |
| Permanent rate-limit lockout from non-atomic INCR+EXPIRE on cache restart | Denial of Service (self-DoS) | Atomic Lua script for INCR+EXPIRE — THIS IS HOTFIX-03 |
| Silent session loss / dangling refresh from non-transactional rotation | Tampering / Repudiation | Wrap delete-old + insert-new in single GORM transaction — THIS IS HOTFIX-05 |
| Silently misconfigured production with empty env defaults | Tampering (deploy-time) | Aggregate required-env validator; exit 1 with named missing keys — THIS IS HOTFIX-08 |
| Sequential scan on auth-hot-path lookup degrades to brownout under load | Denial of Service (load-induced) | UNIQUE index on `sessions.refresh_token_hash` — THIS IS HOTFIX-07 |
| Subscription never expires because expiry isn't persisted | Repudiation / Business Logic Bypass | Schema column + scheduler read (Phase 1) + webhook write (Phase 3) — THIS IS HOTFIX-01 |

**Every Phase 1 hotfix maps to at least one ASVS control and at least one STRIDE pattern.** This is by design — Tranche 0 in MASTER-PLAN.md is explicitly the security-must-fix list.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The migration runner used in this project applies SQL files outside an auto-wrapped transaction (so CONCURRENTLY works). Inferred from numeric-prefix filename convention. | HOTFIX-07 | If wrong, `017_*.sql` fails with "CREATE INDEX CONCURRENTLY cannot run inside a transaction block". Fix: split into 017a (DELETE in tx) + 017b (CREATE INDEX, no tx). Planner should verify by running the migration against a fresh staging DB before declaring HOTFIX-07 done. |
| A2 | `term.ReadPassword` is acceptable to the operator's CLI workflow (echo-off interactive prompt). | HOTFIX-06 | If operator script-automates `createadmin` (e.g., via Ansible), the TTY check fails. Fallback: add `--stdin-pipe` flag that bypasses `term.ReadPassword`. Plan B already documented in "Risks" subsection. |
| A3 | `cfg.JWTSecret` and `cfg.TunnelVLESSUUID` empty-checks at `config.go:68-74` can be left in as defense-in-depth (no breakage). | HOTFIX-08 | If unit tests rely on calling `Load()` without setting JWT_SECRET, the existing test fixtures already set it. Keep the in-Load checks to avoid breaking any test that bypasses `cmd/main.go`. |
| A4 | Fiber v2.52.5's `middleware/requestid` provides the exact "accept-if-present, generate-if-not" behavior CONTEXT.md spec'd. | HOTFIX-04 | [VERIFIED in source — A1-level confidence not needed; this is fact.] No risk; downgraded to verified. |
| A5 | The `users.subscription_expires_at` column already in `migrations/001_initial.sql` is the column the Phase 3 lava.top webhook will write. (No separate `subscriptions.expires_at` is the source of truth.) | HOTFIX-01 | If Phase 3 ends up writing to `subscriptions.expires_at` instead, the scheduler read at `user_repo.go:282` won't see it. **Risk: confirm with operator** that `users.subscription_expires_at` is the canonical source. Quick verification: `grep -rn subscription_expires_at server/api/internal/` — all writes today are to `users` (zero, since Stripe never wrote it, but the model maps to `users`). Low risk. |
| A6 | The audit's "p99 admin request cost" concern for HOTFIX-02 is well-bounded by the fact that admin traffic is < tens of requests per minute today. | HOTFIX-02 | If admin route traffic grows 1000x (improbable), the per-request DB hit becomes noticeable. Mitigation already exists: PERF-04 (cache user existence + tier with TTL ≤ 5s) lands in Phase 6. |
| A7 | `redis.NewScript().Run()` in go-redis v9.18.0 transparently handles `EVAL`-first-then-cache-and-use-`EVALSHA` semantics. | HOTFIX-03 | [VERIFIED in source — go-redis v9 `script.go`.] No risk. |

## Risks & Unknowns

These are items the planner should resolve via quick operator-side or codebase-side checks BEFORE finalizing the per-task plans:

1. **[UNKNOWN] Which migration runner is in use?** Affects HOTFIX-07. Quick resolution: `find server/api -name "Makefile" -o -name "*.sh" | xargs grep -l "migrate\|psql.*-f"`. If raw `psql -f`: file as written works. If `golang-migrate`: may need `--tx-wrap=false` filename suffix or split into 017a/017b. **30-second check.**

2. **[UNKNOWN] Are there existing duplicate `refresh_token_hash` rows in staging/dev?** Affects HOTFIX-07 step 1 (dedupe). Quick resolution: `psql -t -c "SELECT refresh_token_hash, COUNT(*) FROM sessions GROUP BY refresh_token_hash HAVING COUNT(*) > 1 LIMIT 5"`. Production has no paying users so likely 0; staging may have artifacts. **10-second check.**

3. **[KNOWN] HOTFIX-01 requires almost no code change but the commit message must clearly document the no-op nature.** Otherwise reviewers will ask "where's the fix?" The fix is the regression test + the commit-message acknowledgment that schema+scheduler are already correct; the audit's webhook-write fix is intentionally deferred to Phase 3 per D-07. Plan the commit message carefully.

4. **[KNOWN] The `cmd/main.go` middleware ordering is delicate.** After Phase 1: `requestid` (HOTFIX-04 new) → `recover` (existing line 68) → `cors` → `AppVersion` → `RateLimit` → routes. Don't accidentally insert `requestid` after `recover` — panic recovery paths need request_id too.

5. **[KNOWN] `createadmin` CLI breaking change.** Removing `-password` is intentional but should be called out in the commit message and Phase 1 PR description. Anyone with an Ansible playbook, runbook entry, or container-init script that runs `createadmin -password=...` will see breakage. Grep `docs/`, `infrastructure/`, `*.sh`, `Dockerfile*` for `-password=` before commit; update if found.

6. **[KNOWN] `golang.org/x/term` is not yet a direct require.** `go get golang.org/x/term@latest && go mod tidy` adds it. Verify `go build ./...` after.

7. **[OPERATOR CONFIRMATION RECOMMENDED but LOW PRIORITY] A5 above:** Phase 3's lava.top webhook will write `users.subscription_expires_at` (not `subscriptions.expires_at`). Confirm with operator OR planner can read ADR-007 §19 for clarity on which table is the SoT.

8. **[OPERATOR HEAD'S UP] `v2.2.0-hotfix` tag vs `v2.2.0` tag.** CONTEXT.md "specifics" says "Deploy tag is `v2.2.0-hotfix` (or just `v2.2.0` per MASTER-PLAN.md Tranche 0 exit criteria — confirm with planner)." MASTER-PLAN line 29 says `tag v2.2.0`. ROADMAP doesn't specify. **Recommend `v2.2.0-hotfix` for clarity** — Phase 2+ work will need its own tag (`v2.2.0-rc1`, etc.) and reserving the bare `v2.2.0` for the full milestone is conventional.

9. **[KNOWN — no action needed]** Phase 1 does NOT add a `/api/v1/__force-500` debug route. Staging smoke step 2-3 above reference it as a *suggested* test endpoint; planner can either add it (gated to non-prod) or trigger a real 500 from an existing endpoint by, e.g., presenting an invalid refresh token and forcing the GORM error path. Suggest the latter (no new prod surface).

## Recommendations to Planner

For each "Claude's Discretion" knob in CONTEXT.md, here's the concrete pick with rationale:

1. **HOTFIX-02 (admin DB re-read approach):** **Pure DB-every-admin-request.** No Redis cache. Rationale: success criterion #1 mandates "very next request, not five minutes later"; any TTL > 0 introduces a window where demotion isn't honored. Admin traffic is bounded (< tens of req/min); the per-request PK lookup cost is sub-millisecond. If profiling later shows this is hot, PERF-04 in Phase 6 provides the unified user+tier+role cache (TTL ≤ 5s).

2. **HOTFIX-03 (atomic INCR+EXPIRE mechanism):** **Lua `EVAL` via `redis.NewScript().Run()` inline in `cache/redis.go`.** Rationale: canonical atomic pattern; single round-trip; go-redis v9 handles `EVALSHA` caching transparently so there's no perf give-up versus a manual `MULTI/EXEC`; Lua is easier to read at code-review time than a pipelined MULTI/EXEC; matches CONTEXT.md default explicitly.

3. **HOTFIX-06 (createadmin password input):** **`golang.org/x/term.ReadPassword(int(os.Stdin.Fd()))` with `term.IsTerminal` fallback to `bufio.NewReader` when stdin is not a TTY.** Rationale: echo-off interactive prompt for operator UX (sudo-style); pipe-fallback for test automation; cross-platform via `os.Stdin.Fd()` (not `syscall.Stdin`); matches CONTEXT.md default explicitly.

4. **HOTFIX-04 (request-ID library):** **Fiber's built-in `middleware/requestid` with `Generator: utils.UUIDv4` and `Header: fiber.HeaderXRequestID`.** Rationale: zero custom code; the package ALREADY supports "accept-if-present, generate-if-not" out of the box; using UUIDv4 (not the default `utils.UUID`) avoids leaking request count via monotonic IDs.

5. **HOTFIX-08 (validator API shape):** **Two functions, `config.RequireEnv() []string` (returns missing keys) and `config.OptionalEnvWarnings() []string` (returns placeholder/empty optional keys).** Called from `cmd/main.go` in that order, with `logger.Fatal(zap.Strings("missing", ...))` for required and `logger.Warn(zap.Strings("vars", ...))` for optional. Rationale: D-04 mandates single-pass aggregate; separating required (fatal) from optional (warn) keeps the call sites clear and lets Phase 3 simply append to `RequireEnv`'s slice when LAVA_* lands.

6. **HOTFIX-07 (migration shape):** **Single file `017_sessions_refresh_token_hash_unique.sql` with the dedupe DELETE and CREATE UNIQUE INDEX CONCURRENTLY in order, plus a comment block warning the migration runner that CONCURRENTLY cannot run in a transaction.** If A1 (migration runner unwraps individual files) turns out wrong, split into `017a_sessions_dedupe.sql` and `017b_sessions_refresh_token_hash_unique.sql`. Rationale: single file is cleaner git history; split is a 30-second fallback.

7. **Testing granularity:** **One test file per hotfix (extending existing siblings where present), unit-first, integration only where transactional/middleware behavior matters.** Specifically:
   - HOTFIX-06: shell smoke + unit test in `cmd/createadmin/main_test.go`.
   - HOTFIX-08: unit tests in `internal/config/config_test.go`.
   - HOTFIX-04: integration test using Fiber `app.Test()` in `internal/handler/errorhandler_test.go`.
   - HOTFIX-02: integration in `internal/middleware/admin_test.go` (extend).
   - HOTFIX-03: unit with miniredis in `internal/cache/redis_test.go`.
   - HOTFIX-05: integration with sqlite in `internal/handler/auth_test.go` (extend).
   - HOTFIX-01: unit with sqlite in `internal/repository/user_repo_subscription_test.go`.
   - HOTFIX-07: shell + EXPLAIN smoke in `server/api/scripts/smoke_test_session_index.sh` (real Postgres needed).

8. **Commit message convention:** Each commit message MUST reference the audit ID (`CRIT-XX` / `S?-?` / `Perf #?`) AND the HOTFIX-XX ID so traceability survives. Example: `fix(api): scrub 5xx error bodies + request-ID middleware (HOTFIX-04, CRIT-04, S9-1)`. This makes the audit-to-code link greppable.

9. **Pre-tag manual smoke MANDATORY.** Don't tag `v2.2.0-hotfix` purely on green CI. The 10-step staging checklist in §Validation Architecture is the gate. The HOTFIX-02 and HOTFIX-05 fixes in particular exercise live DB+Redis behavior that unit tests with sqlite/miniredis cannot fully prove.

## Sources

### Primary (HIGH confidence)
- /Users/abdunabi/Desktop/vpn/server/api/cmd/main.go — re-read 1-266 for ErrorHandler wiring, middleware chain, scheduler start
- /Users/abdunabi/Desktop/vpn/server/api/internal/handler/health.go — re-read 155-172 for ErrorHandler body (audit citation confirmed)
- /Users/abdunabi/Desktop/vpn/server/api/internal/middleware/admin.go — re-read 1-18 (whole file)
- /Users/abdunabi/Desktop/vpn/server/api/internal/middleware/auth.go — re-read 1-107 for how role is set, FindUserByID already called
- /Users/abdunabi/Desktop/vpn/server/api/internal/middleware/ratelimit.go — re-read 1-150 for IncrRateLimit caller
- /Users/abdunabi/Desktop/vpn/server/api/internal/cache/redis.go — re-read 1-96 for IncrRateLimit and BlacklistToken
- /Users/abdunabi/Desktop/vpn/server/api/internal/handler/auth.go — re-read 1-543 for RefreshToken handler, storeRefreshSession
- /Users/abdunabi/Desktop/vpn/server/api/internal/config/config.go — re-read 1-113 for current env loading
- /Users/abdunabi/Desktop/vpn/server/api/cmd/createadmin/main.go — re-read 1-82
- /Users/abdunabi/Desktop/vpn/server/api/internal/scheduler/scheduler.go — re-read 1-141 for DowngradeExpiredSubscriptions call site
- /Users/abdunabi/Desktop/vpn/server/api/internal/repository/user_repo.go — re-read 38-49 (FindUserByID), 270-288 (DowngradeExpiredSubscriptions)
- /Users/abdunabi/Desktop/vpn/server/api/internal/model/user.go — confirms SubscriptionExpiresAt *time.Time exists
- /Users/abdunabi/Desktop/vpn/server/api/migrations/001_initial.sql — confirms users.subscription_expires_at column AND sessions table missing the UNIQUE index
- /Users/abdunabi/Desktop/vpn/server/api/go.mod — confirms versions: fiber v2.52.5, redis/go-redis/v9 v9.18.0, gorm v1.30.0, zap v1.27.0
- $GOPATH/pkg/mod/github.com/gofiber/fiber/v2@v2.52.5/middleware/requestid/{config.go,requestid.go} — confirms requestid middleware exists with the exact "accept-if-present, generate-if-not" behavior
- docs/audit/MASTER-PLAN.md §Tranche 0 — file:line citations for every hotfix
- docs/audit/CODE-REVIEW.md CRIT-01..04, HIGH-08 — re-read
- docs/audit/SECURITY-AUDIT.md S1-1, S2-1, S2-2, S3-4, S3-5, S9-1 — re-read
- docs/audit/PERFORMANCE-AUDIT.md Perf #1 — re-read for EXPLAIN expectations

### Secondary (MEDIUM confidence)
- WebFetch pkg.go.dev/golang.org/x/term — confirmed `func ReadPassword(fd int) ([]byte, error)` signature and cross-platform behavior

### Tertiary (LOW confidence)
- None — every Phase 1 claim is verified against source or official docs.

## Metadata

**Confidence breakdown:**
- Per-Hotfix Recipes — HIGH — every file:line citation re-verified; every library API confirmed in source at $GOPATH; Fiber's requestid middleware behavior confirmed by reading the source
- Cross-Hotfix Interactions — HIGH — derived from re-reading cmd/main.go in full
- Library Versions — HIGH — read from go.mod and confirmed against $GOPATH/pkg/mod
- Validation Architecture — HIGH — patterns derived from existing `*_test.go` files in tree
- Risks & Unknowns — MEDIUM — A1 (migration runner) and A5 (canonical expires_at table) are confidence-LOW assumptions flagged for the planner; everything else is verified.

**Research date:** 2026-05-22
**Valid until:** 2026-06-22 (30 days — backend stack is stable; only invalidator is if go-redis/Fiber/GORM ship breaking changes, which would take weeks)
