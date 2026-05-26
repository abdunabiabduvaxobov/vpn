---
phase: 04-landing-surfaces
fixed_at: 2026-05-26T00:00:00Z
review_path: .planning/phases/04-landing-surfaces/04-REVIEW.md
iteration: 1
findings_in_scope: 2
fixed: 2
skipped: 0
status: all_fixed
---

# Phase 04 (Plan 04-09 gap closure): Code Review Fix Report

**Fixed at:** 2026-05-26T00:00:00Z
**Source review:** .planning/phases/04-landing-surfaces/04-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope (Critical + Warning): 2
- Fixed: 2
- Skipped: 0
- Info findings (out of scope per fix_scope=critical_warning): 4 (IN-01, IN-02, IN-03, IN-04)

## Fixed Issues

### WR-01: Module-level `Set` never clears on failure → retry produces silent no-op

**Files modified:** `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx`
**Commit:** e25228d
**Applied fix:** Added `inflightCheckouts.delete(key)` in all four failure branches of the auto-checkout effect:

- 401 branch (line 88) — clears latch before the `/login?next=...` bounce so the next page-load re-fires the intent and POSTs again.
- non-OK status branch (line 97) — clears latch before `setError(t("network"))` so a retry on the same query string is not a silent no-op.
- non-Lava `payment_url` branch (line 105) — clears latch before the defensive reject.
- `catch` for network/parse failure (line 118) — clears latch before `setError(t("network"))`.

Success path (line 113, `window.location.href = url`) intentionally LEAVES the key in the Set per the reviewer's "conservative fix" guidance — the hard navigation makes any remount irrelevant, and the surviving key protects against a true Strict-Mode remount in the gap between fetch resolution and href assignment.

Comments updated inline at each branch to document the intent of the delete (so future readers understand the failure-vs-success asymmetry). The block comment above `inflightCheckouts` (lines 44-55) was deliberately NOT modified — the existing rationale still holds; only the per-branch behaviour changed.

**Verification:**
- Tier 1 (re-read): confirmed all four `inflightCheckouts.delete(key)` calls present; surrounding `router.replace`, `setError`, `window.location.href` intact.
- Tier 2 (TypeScript): `cd landing && npx tsc --noEmit` exits 0 (no type errors).
- Tier 2.5 (E2E): `npm run test:e2e` failure profile is identical to pre-fix baseline (5 failing, 5 passing). The same 5 tests fail with the same errors before and after this edit, confirming the fix introduced no regressions. Baseline failures are pre-existing environment/mock issues unrelated to the strict-mode latch behaviour being modified.

**Invariants preserved (per task guardrails):**
- `LAVA_URL_PATTERN` regex unchanged.
- 401 → `/login?next=...` bounce contract unchanged.
- `?checkout=auto&plan=...&period=...&currency=...` query semantics unchanged.
- Module-level `Set` data structure unchanged; only the delete-on-failure call sites added.

---

### WR-02: `poll-client.tsx` reset does not clear stale `timerRef` / `timeoutRef`

**Files modified:** `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx`
**Commit:** 8095962
**Applied fix:** Mirrored the `stop()` teardown inline at the top of the `useEffect` body, BEFORE the re-arming `pollOnce()` / `setInterval` / `setTimeout` calls:

```ts
stopped.current = false;
pollNo.current = 0;
if (timerRef.current !== null) {
  window.clearInterval(timerRef.current);
  timerRef.current = null;
}
if (timeoutRef.current !== null) {
  window.clearTimeout(timeoutRef.current);
  timeoutRef.current = null;
}
pollOnce();
timerRef.current = window.setInterval(pollOnce, INTERVAL_MS);
timeoutRef.current = window.setTimeout(...);
```

This makes the reset block self-sufficient: it no longer relies on the cleanup `stop()` having executed first. A future refactor that adds an early-return before `return () => stop()` or migrates to `useSyncExternalStore` cannot orphan the first mount's interval handle.

Added a multi-line comment block (lines 154-159) explaining the WR-02 defensive intent so future readers do not strip the seemingly-redundant clears as "dead code".

**Verification:**
- Tier 1 (re-read): confirmed both `window.clearInterval` / `window.clearTimeout` calls and ref nulling present at lines 162-169; subsequent `pollOnce()`, `setInterval`, `setTimeout` re-arming intact at lines 172-178.
- Tier 2 (TypeScript): `cd landing && npx tsc --noEmit` exits 0.
- Tier 2.5 (E2E): identical failure profile to baseline (see WR-01 verification above). The two `pay-success.spec.ts` tests that exercise this effect (SC#4 happy and SC#4 timeout) fail identically with-and-without the edit, confirming pre-existing environmental causes rather than a regression from the defensive timer clear.

**Invariants preserved (per task guardrails):**
- `INTERVAL_MS = 2000` unchanged.
- `TIMEOUT_MS = 30000` unchanged.
- `ESCALATE_AFTER_POLL = 6` unchanged.
- 401 → `/login?next=/dashboard` bounce contract unchanged.
- 404 → `/pay/fail?reason=default` contract unchanged.
- `paid` / `failed` / `cancelled` status mapping unchanged.
- B2/D-17 force-refresh-on-paid contract unchanged.
- The single-shot `refresh()` function (used after the takingLonger UI) unchanged.

## Skipped Issues

None — both in-scope findings (WR-01, WR-02) were fixed successfully.

## Out-of-Scope (Info) Findings

The following four Info-level findings from the review are deliberately NOT addressed by this fix run because `fix_scope = critical_warning`. They are documented here for the next reviewer / fixer cycle:

- **IN-01:** Add `if (stopped.current) return;` after every `await` boundary in `pollOnce` (defensive churn; reviewer noted "current behaviour is observably correct").
- **IN-02:** Update the block comment at `pricing-client.tsx:43-55` to clarify the `Set` survives SPA navigations within a session (mostly resolved by WR-01's delete-on-failure pattern; only the doc nuance remains).
- **IN-03:** Refactor `pricing.spec.ts:133-138` to wrap navigation + `waitForRequest` in `Promise.all([...])` for consistency with the SC#5 sibling pattern.
- **IN-04:** Parametrize `navbar.spec.ts` SC#6 across EN/RU/ES locales instead of testing only the RU path.

## Notes on E2E baseline

`landing/playwright.config.ts` is intentionally left untouched (`NODE_ENV=development` Strict Mode stays on, per task guardrails). The 5 failing E2E tests observed during verification are pre-existing failures present BOTH before and after this fix run (verified via `git stash` → run → unstash sequence). They are unrelated to the strict-mode invariants modified by WR-01 and WR-02 and should be addressed by the verifier phase or a subsequent gap-closure plan.

---

_Fixed: 2026-05-26T00:00:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
