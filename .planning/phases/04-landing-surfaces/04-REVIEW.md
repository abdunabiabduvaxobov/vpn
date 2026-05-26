---
phase: 04-landing-surfaces
reviewed: 2026-05-26T00:00:00Z
depth: standard
files_reviewed: 5
files_reviewed_list:
  - landing/src/app/[locale]/(app)/pay/success/poll-client.tsx
  - landing/src/app/[locale]/(app)/pricing/pricing-client.tsx
  - landing/src/components/app/user-menu.tsx
  - landing/e2e/navbar.spec.ts
  - landing/e2e/pricing.spec.ts
findings:
  critical: 0
  warning: 2
  info: 4
  total: 6
status: issues_found
---

# Phase 04 (Plan 04-09 gap closure): Code Review Report

**Reviewed:** 2026-05-26T00:00:00Z
**Depth:** standard
**Files Reviewed:** 5
**Status:** issues_found

## Summary

The five-file gap closure resolves React Strict Mode failures that VERIFICATION
04 surfaced (4 failing observable truths) by:

1. Resetting `stopped.current` and `pollNo.current` at the top of the
   `useEffect` body in `poll-client.tsx`.
2. Replacing a per-component `useRef(false)` latch with a module-level
   `Set<string>` keyed by `${plan}|${period}|${currency}` in
   `pricing-client.tsx`.
3. Adding `data-testid="sign-out-button"` to the UserMenu sign-out submit
   button and switching the navbar test to `page.getByTestId(...)`.
4. Swapping `page.context().cookies()` for `page.evaluate(() => document.cookie)`
   in the SC#5 cookie-persistence assertion.

**High-level assessment.** All four changes are individually well-reasoned and
preserve the existing invariants (LAVA_URL_PATTERN unchanged, 401 → /login
bounce intact, 30s polling cap intact, /api/auth/logout form fallback intact,
HttpOnly boundary intact — `document.cookie` cannot see `rv_at` regardless).
The `"use client"` directive on `pricing-client.tsx` confines the module-level
`Set` to the browser bundle, eliminating the SSR cross-user-leakage concern.

**Two warnings deserve attention before launch:**

- The `Set` is documented as "intentionally never cleared (per-page-load
  lifetime)" but it is never re-cleared on **success** either, which means a
  user who returns to the same `(plan, period, currency)` from the browser
  back button after a transient network failure will silently no-op instead of
  retrying. This trades a sharp edge (duplicate POST) for a softer edge
  (mysterious silence). Worth a one-line addition in the catch / non-2xx
  branches.
- The `poll-client.tsx` reset pattern does NOT clear the previous interval/
  timeout timer refs at the top of the effect. Under React Strict Mode the
  cleanup `stop()` clears them, so this is benign today, but if a future
  refactor moves `stop()` out of the cleanup path the `setInterval` handle
  from the first mount would leak.

Four info-level items relate to documentation accuracy, locale-test coverage,
and minor robustness opportunities.

## Critical Issues

None. No security regressions, no data-loss risks, no crash paths introduced.
The HttpOnly boundary is respected (the new `document.cookie` test path can
only read non-HttpOnly cookies, and `pricing_currency` is non-HttpOnly by
design — confirmed by the test passing under Strict Mode). The
`LAVA_URL_PATTERN` regex is unchanged and the 401-bounce / 30s timeout / form
fallback contracts are all intact.

## Warnings

### WR-01: Module-level `Set` never clears on failure → retry produces silent no-op

**File:** `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx:56-70`

**Issue:** `inflightCheckouts.add(key)` is called BEFORE the fetch, but the
key is never removed in any of the error branches:

- 401 → `router.replace("/login?next=...")` (line 88), no delete.
- non-OK status → `setError(t("network"))` (line 92), no delete.
- non-Lava `payment_url` → `setError(t("network"))` (line 99), no delete.
- `catch` for network/parse failure → `setError(t("network"))` (line 109), no
  delete.

The PLAN says the Set is "intentionally never cleared (per-page-load lifetime)",
which correctly protects the success path (after `window.location.href = url`
the page is gone anyway). But for ALL failure paths the user is left on the
same URL with the same `?checkout=auto&plan=...&period=...&currency=...` query
string. If they click "Try again" (or the browser triggers a re-mount via
React DevTools, locale swap, etc.), the effect re-runs, the key is found in
the Set, and the POST silently no-ops — the user sees the inline
"errors.network" toast forever with no way to retry without changing the URL.

This is a behavioural regression vs. the old `useRef(false)` latch: the old
latch died with the component, so a remount-after-failure would re-fire. The
new latch survives ALL remounts on the same page load.

**Severity rationale:** Not critical because the user CAN reload the page or
go back to /pricing and click the CTA again — but the inline-error path is
explicitly designed to keep them on the page ("so the user knows the click was
registered and they can retry" — file docstring line 17). The retry path is
broken.

**Fix:**

```ts
(async () => {
  try {
    const r = await fetch("/api/v1/checkout", { /* ... */ });
    if (r.status === 401) {
      // The next page-load re-fires intent; safe to clear.
      inflightCheckouts.delete(key);
      router.replace(`/login?next=${encodeURIComponent(next)}`);
      return;
    }
    if (!r.ok) {
      inflightCheckouts.delete(key);
      setError(t("network"));
      return;
    }
    const json = await r.json().catch(() => null);
    const url = json?.payment_url;
    if (typeof url !== "string" || !LAVA_URL_PATTERN.test(url)) {
      inflightCheckouts.delete(key);
      setError(t("network"));
      return;
    }
    // SUCCESS — leave the key in the Set. The hard navigation makes any
    // re-mount irrelevant, and this prevents a double-POST race if the
    // navigation is slow (a true Strict Mode remount in the gap between
    // fetch resolution and href assignment).
    window.location.href = url;
  } catch {
    inflightCheckouts.delete(key);
    setError(t("network"));
  }
})();
```

Alternative: clear the key always (success too). The Strict-Mode simulated
remount only occurs once, synchronously, on initial mount in dev — it does
NOT happen after the fetch microtask resolves. So clearing on success is also
safe, just leaves a narrow theoretical double-fire window if a real remount
happens between fetch resolution and the `window.location.href` assignment.
The conservative fix above (delete only on failure) preserves the documented
"protect the actual POST" intent.

---

### WR-02: `poll-client.tsx` reset does not clear stale `timerRef` / `timeoutRef`

**File:** `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx:147-166`

**Issue:** The new lines 153-154 reset `stopped.current` and `pollNo.current`,
but NOT `timerRef.current` or `timeoutRef.current`. Today this is benign
because Strict Mode's simulated cleanup calls `stop()` (line 164) which clears
both timers before the real-mount pass runs the effect again. So the new
effect body overwrites the refs with fresh handles on line 158/159.

However, this creates a fragile coupling: if a future refactor moves the
cleanup logic (e.g., switches to `useSyncExternalStore`, or stops calling
`stop()` in the cleanup, or adds an early-return path before
`return () => stop()`), the first-mount's `setInterval` handle would leak —
because the second-mount effect overwrites the ref WITHOUT clearing the
underlying interval. The result is a phantom interval ticking forever (or
until the page unloads), invisibly calling `pollOnce()` against an unmounted
component, and `setView` would no-op-warn in React 19.

**Severity rationale:** Currently benign — verified by SC#4 happy and SC#4
timeout passing under Strict Mode. But the reset block is now defensive code
whose correctness depends on a non-obvious invariant elsewhere (cleanup always
calls `stop()`). Defence-in-depth should make the reset self-contained.

**Fix:** Mirror the `stop()` call inside the reset to make it idempotent and
self-sufficient:

```ts
useEffect(() => {
  // Strict Mode simulated unmount→remount: cleanup ran stop() which cleared
  // the timers AND set stopped.current=true. Reset both the flag AND defensively
  // clear any stale handles before re-arming, so this block does not rely on
  // cleanup having executed.
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
  timeoutRef.current = window.setTimeout(() => { /* ... */ }, TIMEOUT_MS);
  return () => stop();
  // eslint-disable-next-line react-hooks/exhaustive-deps
}, []);
```

## Info

### IN-01: Pending `pollOnce()` microtask from Strict Mode first-pass still resolves against the unmounted closure

**File:** `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx:90-145`

**Issue:** Under Strict Mode, the first effect pass calls `pollOnce()` (line
157) synchronously. That call awaits `fetch(...)`. If the fetch resolves
AFTER the cleanup ran but BEFORE the real-mount's effect body resets
`stopped.current = false`, the in-flight `pollOnce` will see
`stopped.current === true` (set by cleanup `stop()`) only on its next early-
return check — which only happens at line 91, NOT after the `await`. After
the await, the resolved branch will call `router.replace(...)`,
`setView(...)`, or `forceRefreshForNewPlanId()` on what React considers an
unmounted component.

In practice this is harmless: React 19 just no-ops `setView` on an unmounted
component without warnings (unlike React 18), the router replace is a no-op
on an unmounted Next router (the next mount will overwrite), and the
forceRefresh side-effect on cookies is idempotent. But it is a code smell.

**Fix:** Add a stopped check after every `await` boundary in `pollOnce`:

```ts
const r = await fetch(url, { credentials: "same-origin" });
if (stopped.current) return;  // <-- add
if (r.status === 401) { /* ... */ }
// ...
const json = await r.json().catch(() => null);
if (stopped.current) return;  // <-- add
```

Skip if the team considers this churn — current behaviour is observably correct.

---

### IN-02: Module-level `Set` survives Next.js fast-refresh, not just Strict Mode

**File:** `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx:56`

**Issue:** A module-level `Set` in a `"use client"` module is preserved across
React Strict Mode remounts (correct) AND across Next.js Fast Refresh module
re-evaluations (only if the file itself doesn't change; if you edit pricing-
client.tsx in dev the module reloads and the Set is freshly empty). It is
NOT preserved across full route navigations in development OR production
because Next.js client navigations don't re-evaluate the module, but a hard
reload (F5) does.

The PLAN's comment "per-page-load lifetime" is accurate for hard navigations,
slightly off for SPA navigations (Set persists across `router.push`
back-and-forth within the same SPA session). This means: a user who hits
/pricing → clicks Pro → fails → router-back to /pricing → clicks Pro again
with the SAME (plan,period,currency) gets the silent-no-op of WR-01 even if
WR-01's failure-path delete is added — because the second `router.push`
mounts a fresh PricingClient that re-runs the effect, but the Set still holds
the key from the previous attempt's success-path.

**Fix:** Mostly addressed by WR-01's "delete on failure" pattern. For success
path the Set persistence is desirable (it actually does prevent the double-
fire during the navigation window). Consider documenting this nuance in the
comment block (lines 43-55).

---

### IN-03: `mockPlans` and `?checkout=auto` test uses `await page.goto(...)` without `waitUntil`, which can race with the auto-fire effect

**File:** `landing/e2e/pricing.spec.ts:133-138`

**Issue:** SC#2 sets up two `page.waitForRequest` promises THEN calls
`page.goto(...)` without `{ waitUntil: "networkidle" }` or a `Promise.all`.
This works because `page.goto()` returns when the main document loads (not
when JS settles), and Playwright's `waitForRequest` is set up before navigation
starts. But the pattern is fragile: if Playwright's auto-wait ever changes its
"main document load" definition, or if Next.js's hydration timing shifts, the
test could miss the request. The SC#5 sibling test at line 25 explicitly uses
`{ waitUntil: "networkidle" }` — consistency would help.

**Fix:** Make the navigation and the request waits explicitly concurrent:

```ts
await Promise.all([
  checkoutPosted,
  lavaRequested,
  page.goto(
    "/ru/pricing/?checkout=auto&plan=pro&period=monthly&currency=USD",
  ),
]);
```

This is the documented Playwright pattern for "fire navigation, wait for
events" and is more obvious about intent.

---

### IN-04: Navbar test asserts only the RU locale path

**File:** `landing/e2e/navbar.spec.ts:43, 56`

**Issue:** Both SC#6 tests hit `/ru/pricing/` exclusively. The role-name
regexes try to be tri-lingual (`/Тарифы|Pricing|Precios/`), but the route
itself is RU-only. If the locale prefix routing breaks for EN or ES (a
common Next.js misconfiguration), this test won't catch it. The
`data-testid="sign-out-button"` is locale-agnostic now (good!), but the
page being tested is locale-specific.

**Fix:** Either drop the tri-lingual regexes (we only ever hit RU, so just
match `/Тарифы/` and `/Войти/` exactly) OR parametrize the test across
locales:

```ts
for (const locale of ["en", "ru", "es"] as const) {
  test(`SC#6 logged-out [${locale}]: navbar shows Pricing + Login`, async ({ page }) => {
    await mockPlans(page);
    await page.goto(`/${locale}/pricing/`);
    // ... assertions
  });
}
```

Defer to the team's testing-time budget — current coverage is acceptable
because the navbar component itself is i18n-shared.

---

_Reviewed: 2026-05-26T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
