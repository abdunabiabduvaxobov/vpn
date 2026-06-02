---
phase: 08-cleanup-hardening
plan: 05
subsystem: backend / build-supply-chain
tags: [stripe-removal, dependency-cleanup, regression-fence, HARD-01, SC1]
requires:
  - "Wave 1 merged (08-01 test infra, 08-03 main.go env) — base 02e8ea6"
  - "migration 020_lava_payments.sql already dropped subscriptions.stripe_id (Phase 3)"
provides:
  - "go.mod/go.sum free of github.com/stripe/stripe-go (HARD-01)"
  - "zero residual stripe references in production .go (only allowlisted test assertions remain)"
  - "durable TestNoStripeReferences fence keeping SC#1 closed against regression"
affects:
  - "server/api build supply chain (one fewer dep for govulncheck — HARD-09)"
tech-stack:
  added: []
  patterns:
    - "exec.Command(grep) Go test as a durable repo-invariant regression fence"
key-files:
  created:
    - server/api/internal/handler/stripe_removal_test.go
  modified:
    - server/api/go.mod
    - server/api/go.sum
    - server/api/internal/config/config.go
    - server/api/internal/config/config_test.go
    - server/api/internal/handler/admin_test.go
    - server/api/internal/handler/payment.go
    - server/api/internal/middleware/version.go
    - server/api/internal/model/subscription.go
    - server/api/internal/repository/subscription_repo.go
    - server/api/internal/repository/user_repo.go
    - server/api/internal/repository/user_repo_subscription_test.go
    - server/api/cmd/main.go
decisions:
  - "Removed Stripe config struct fields + env loading + OptionalEnvWarnings entries entirely (provider leaves in Phase 8) rather than leaving dead StripeKey fields — closes the grep gate at the source."
  - "Allowlisted migrations/migrations_test.go in the fence: its stripe_id column-absence assertion is the intentional SC#1 EVIDENCE the column is dropped (D-11), not a regression."
  - "go.sum stripe-go lines removed by hand (leaf dep, no other module depends on it) because `go mod tidy` is blocked by the sandbox network policy — net effect identical."
metrics:
  duration: ~25m
  tasks: 2
  files: 13
  completed: 2026-06-02
---

# Phase 8 Plan 05: Stripe Removal + Durable Regression Fence Summary

Removed the last residual Stripe code and the `stripe-go` dependency, then locked SC#1 shut with a `TestNoStripeReferences` Go test that re-runs the grep at every `go test`.

## What Shipped

**Task 1 — Stripe removal (commit 7f9a34c):**
- Deleted `github.com/stripe/stripe-go/v81 v81.4.0` from `go.mod` and purged its two `go.sum` lines.
- Removed the `StripeKey` / `StripeWebhookSecret` / `StripePricePremium` / `StripePriceUltimate` config struct fields, their `getEnv` loads in `Load()`, and the four `STRIPE_*` entries in `OptionalEnvWarnings()` (kept the legitimate Apple `.p8` entries).
- Rewrote `TestOptionalEnvWarnings_FlagsPlaceholders` → `TestOptionalEnvWarnings_FlagsUnsetOptionalKeys`, exercising the Apple `.p8` keys (preserves the placeholder-flagging coverage without any Stripe reference).
- Removed the `stripe_id TEXT` column from the `admin_test.go` subscriptions fixture.
- Deleted the stale `var _ = fmt.Sprintf` compile-shim + its now-unused `"fmt"` import in `payment.go`.
- Reworded stale Stripe comments/strings in `version.go`, `subscription.go`, `subscription_repo.go`, `user_repo.go` (×2), `user_repo_subscription_test.go`, and `cmd/main.go` (×3 incl. the WARN string).
- No new migration: `subscriptions.stripe_id` was already dropped by migration 020 (D-01 verify-only); the `migrations_test.go` absence assertion still passes and stays as the SC#1 evidence.

**Task 2 — Durable fence (commit a2b017e):**
- Created `server/api/internal/handler/stripe_removal_test.go` with `TestNoStripeReferences`.
- It resolves the repo `server/` root by walking up the directory tree (covers BOTH Go modules — `api` and `tunnel`), runs `grep -rniI --include=*.go stripe`, allowlists exactly `stripe_removal_test.go` (the search literal) and `migrations_test.go` (the intentional column-absence assertion), and `t.Fatal`s naming any offending `file:line`.
- `t.Skip`s when `grep` is unavailable (CI ubuntu-latest always has grep, so the gate is live in CI).
- Proven RED by temporarily re-adding a `stripe_id` fixture line (test failed naming `admin_test.go:345`), then reverted to GREEN.

## Verification

- `grep -rniI stripe server/api/{internal,cmd,migrations} --include='*.go'` → only the two allowlisted files (fence + migrations_test absence assertion); zero production hits.
- `grep -ni stripe go.mod go.sum` → 0 hits.
- `go build ./...` → exits 0.
- `go test ./internal/handler/... -run TestNoStripeReferences` → PASS (GREEN); proven RED on reintroduced reference then reverted.
- `go test ./internal/config/...` → ok (incl. rewritten OptionalEnvWarnings test).
- `go test ./internal/handler/... -run 'Admin|Payment|Webhook'` and `./internal/repository/...` → ok.

## Deviations from Plan

### Auto-fixed Issues / Scope Corrections

**1. [Rule 1 - Bug / scope] Plan's residual-stripe inventory was stale; cleaned the actual full surface**
- **Found during:** Task 1 (initial grep).
- **Issue:** The plan's `<interfaces>` listed "exactly 5" residual `.go` hits. After the Wave-1 merge the real surface was larger: `config.go` (4 struct fields + 4 env loads + 4 `OptionalEnvWarnings` entries), `config_test.go` (a fully Stripe-based test + header comment), `payment.go` (a `var _ = fmt.Sprintf` Stripe-comment shim), `subscription_repo.go`, `user_repo.go` (×2), `user_repo_subscription_test.go`, and `cmd/main.go` (×3, not just line 102). The plan's must_have is "grep returns zero hits", so all were removed to honor the must_have over the stale line list.
- **Files modified:** see key-files.modified.
- **Commit:** 7f9a34c

**2. [Rule 3 - Blocking] `go mod tidy` blocked by sandbox; removed go.sum lines by hand**
- **Issue:** The sandbox denies `go mod tidy` (network policy). `stripe-go` is a leaf dependency (unimported, nothing else requires it), so its two `go.sum` lines were removed directly. The net result is identical to `go mod tidy`, confirmed by a clean `go build ./...` and zero `grep stripe go.mod go.sum` hits.
- **Commit:** 7f9a34c

**3. [Allowlist correction] `migrations_test.go` lives in `server/api/migrations/`, not the handler dir**
- The plan referenced `internal/handler/migrations_test.go:203-207`; the file is actually `server/api/migrations/migrations_test.go`. The fence's by-basename allowlist matches it correctly regardless of directory, and the column-absence assertion (the SC#1 evidence) is preserved unchanged.

### Out-of-scope failures (NOT fixed — logged to deferred-items.md)

- `TestGenerateTokens_RefreshIsOpaque` (auth_opaque_refresh_test.go) is a sibling Wave-2 HARD-03 RED test (opaque refresh tokens) — untouched by this plan.
- Redis/Postgres-dependent handler + migration tests fail on missing local infra (`connection refused`, `relation does not exist`, `plan_id not-null`) — environmental, pre-existing (DEF-08-W1-A, DEF-08-02-A), independent of stripe.

## Known Stubs

None.

## Threat Flags

None — this plan SHRINKS the threat surface (removed unused dep T-08-01, installed the T-08-01c regression fence). No new network endpoints, auth paths, or trust-boundary surface introduced.

## Self-Check: PASSED

- FOUND: server/api/internal/handler/stripe_removal_test.go
- FOUND: commit 7f9a34c (Task 1)
- FOUND: commit a2b017e (Task 2)
- VERIFIED: go.mod/go.sum stripe-free; fence GREEN; build clean.
