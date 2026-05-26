---
phase: 04-landing-surfaces
plan: 09
type: execute
wave: 1
depends_on: []
gap_closure: true
files_modified:
  - landing/src/app/[locale]/(app)/pricing/pricing-client.tsx
  - landing/src/app/[locale]/(app)/pay/success/poll-client.tsx
  - landing/src/components/app/user-menu.tsx
  - landing/e2e/navbar.spec.ts
  - landing/e2e/pricing.spec.ts
autonomous: true
requirements:
  - WEB-04
  - WEB-05
  - WEB-06
  - WEB-09
tags: [gap-closure, strict-mode, react-19, playwright, e2e, web-04, web-05, web-06, web-09]

must_haves:
  truths:
    - "SC#2 — Logged-in user with ?checkout=auto on /pricing causes POST /api/v1/checkout to fire within ~one HTTP round-trip and navigation request to gate.lava.top is observed; works under React Strict Mode (NODE_ENV=development)"
    - "SC#4 happy — /pay/success polls /api/v1/invoices/:id, observes status=paid on poll #2, fires POST /api/v1/auth/refresh BEFORE the active view, and shows 'Pro is active!'; works under React Strict Mode"
    - "SC#4 timeout — /pay/success keeps polling 'pending' for the full 30s window, then renders the takingLonger UI; works under React Strict Mode"
    - "SC#5 cookie — clicking the USD chip in CurrencySwitcher persists pricing_currency=USD as a cookie observable from the page's own document.cookie within 10s of the click"
    - "SC#6 logged-in — after clicking the avatar trigger in NavbarApp, the Sign-out submit button inside the UserMenu Popover Portal is discoverable by a data-testid locator within 5s"
  artifacts:
    - path: "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx"
      provides: "Strict-Mode-safe checkout one-shot guard using a module-level Set keyed by ${plan}|${period}|${currency}"
      contains: "inflightCheckouts"
    - path: "landing/src/app/[locale]/(app)/pay/success/poll-client.tsx"
      provides: "Strict-Mode-safe polling lifecycle that resets the stopped/pollNo refs at the top of useEffect before pollOnce()"
      contains: "stopped.current = false"
    - path: "landing/src/components/app/user-menu.tsx"
      provides: "Stable Playwright locator for the Sign-out submit button"
      contains: "data-testid=\"sign-out-button\""
    - path: "landing/e2e/navbar.spec.ts"
      provides: "Updated SC#6 logged-in locator using getByTestId for the Sign-out button inside the Popover Portal"
      contains: "getByTestId(\"sign-out-button\")"
    - path: "landing/e2e/pricing.spec.ts"
      provides: "Updated SC#5 cookie assertion using page.evaluate(() => document.cookie) instead of context.cookies()"
      contains: "document.cookie"
  key_links:
    - from: "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx"
      to: "/api/v1/checkout"
      via: "fetch in useEffect after module-level Set guard"
      pattern: "inflightCheckouts\\.add\\("
    - from: "landing/src/app/[locale]/(app)/pay/success/poll-client.tsx"
      to: "/api/v1/invoices/"
      via: "pollOnce() inside useEffect after stopped.current = false reset"
      pattern: "stopped\\.current = false"
    - from: "landing/src/components/app/user-menu.tsx"
      to: "landing/e2e/navbar.spec.ts"
      via: "data-testid='sign-out-button' on the form submit button + getByTestId in the spec"
      pattern: "sign-out-button"
    - from: "landing/src/components/app/currency-switcher.tsx"
      to: "landing/e2e/pricing.spec.ts SC#5"
      via: "document.cookie write inspected via page.evaluate"
      pattern: "page\\.evaluate.*document\\.cookie"
---

<objective>
Close the 4 failing observable truths from 04-VERIFICATION.md so all Phase 4 E2E success criteria (SC#2, SC#4 happy + timeout, SC#5 cookie, SC#6 logged-in) pass under the existing `NODE_ENV=development` Playwright topology that intentionally activates React Strict Mode.

Three of the four gaps share the same root cause: `useRef`-based one-shot guards persist their value across React Strict Mode's simulated unmount + remount cycle. The cleanup function from the first pass sets the guard to "done" before the real mount runs, so the effect body's early-return short-circuits the work (no POST, no polling). The fourth gap is a brittle Playwright locator that does not survive base-ui's Popover Portal DOM structure.

Purpose: ship the four defensive fixes that turn 4 failing E2E truths into 4 passing truths, advancing Phase 4 from 4/6 truths verified to 6/6 (with SC#1 still NEEDS-HUMAN for live OAuth credentials — explicitly out of scope here).

Output:
- 3 source files made Strict-Mode-safe and locator-friendly (pricing-client.tsx, poll-client.tsx, user-menu.tsx)
- 2 E2E spec files updated to use stable selectors (navbar.spec.ts, pricing.spec.ts)
- `npm run test:e2e` exits 0 with all 10 tests green (was 5/10 passing, 5/10 failing)
- `npm run build` still exits 0 (no regressions to the standalone bundle)
- No NODE_ENV change in playwright.config.ts (would mask the real Strict-Mode bug rather than fix it)
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/REQUIREMENTS.md
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-VERIFICATION.md
@.planning/phases/04-landing-surfaces/04-07-checkout-pay-success-fail-SUMMARY.md
@.planning/phases/04-landing-surfaces/04-02-app-shell-navbar-primitives-SUMMARY.md
@.planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-SUMMARY.md
@.planning/phases/04-landing-surfaces/04-08-deploy-smoke-tests-SUMMARY.md

<interfaces>
<!-- Key contracts the executor needs. Extracted from the live codebase 2026-05-26. -->
<!-- Executor should use these directly — no codebase exploration needed. -->

From landing/src/app/[locale]/(app)/pricing/pricing-client.tsx (current, broken under Strict Mode):
```typescript
type Props = {
  locale: string;
  plan?: string;
  period?: string;
  checkout?: string;
  currency: string;
};
const LAVA_URL_PATTERN = /^https:\/\/(gate\.|app\.|pay\.)?lava\.top\//;
// useRef one-shot — broken under React Strict Mode
const fired = useRef(false);
useEffect(() => {
  if (fired.current) return;             // ← exits early after Strict Mode cleanup runs
  if (checkout !== "auto" || !plan || !period) return;
  fired.current = true;
  // ... POST /api/v1/checkout
}, [checkout, plan, period, currency, router, t]);
```

From landing/src/app/[locale]/(app)/pay/success/poll-client.tsx (current, broken under Strict Mode):
```typescript
type Props = { invoiceId: string; locale: string };
const INTERVAL_MS = 2000;
const ESCALATE_AFTER_POLL = 6;
const TIMEOUT_MS = 30000;
const stopped = useRef(false);
const pollNo = useRef(0);
function stop() {
  stopped.current = true;
  if (timerRef.current !== null) window.clearInterval(timerRef.current);
  if (timeoutRef.current !== null) window.clearTimeout(timeoutRef.current);
}
async function pollOnce() {
  if (stopped.current) return;            // ← exits early after Strict Mode cleanup runs stop()
  // ... fetch /api/v1/invoices/:id ...
}
useEffect(() => {
  pollOnce();
  timerRef.current = window.setInterval(pollOnce, INTERVAL_MS);
  timeoutRef.current = window.setTimeout(() => { /* takingLonger */ }, TIMEOUT_MS);
  return () => stop();                    // ← Strict Mode runs this between passes, setting stopped.current=true
}, []);
```

From landing/src/components/app/user-menu.tsx (current — Sign-out button has NO testid):
```tsx
<form action="/api/auth/logout" method="POST" className="pt-1">
  <button
    type="submit"
    className="w-full rounded-md px-3 py-2 text-left text-sm text-foreground transition hover:bg-muted"
  >
    {t("signOut")}
  </button>
</form>
```
The button is rendered inside `<Popover.Portal>` → `<Popover.Positioner>` → `<Popover.Popup>` which mounts into a Portal, away from the trigger.

From landing/src/components/app/currency-switcher.tsx (current — code is CORRECT, cookie write is synchronous on click outside useTransition):
```typescript
onClick={() => {
  const secure = typeof location !== "undefined" && location.protocol === "https:" ? "; Secure" : "";
  // This write is synchronous on the click handler, BEFORE start():
  document.cookie = `pricing_currency=${c}; Max-Age=2592000; Path=/; SameSite=Lax${secure}`;
  start(() => {
    const search = new URLSearchParams(typeof window !== "undefined" ? window.location.search : "");
    search.set("currency", c);
    router.replace(`${pathname}?${search.toString()}`);
  });
}}
```
The cookie IS written synchronously. The SC#5 failure is `context.cookies()` not seeing the cookie within 10s. The fix is test-side: use `page.evaluate(() => document.cookie)`.

From landing/e2e/navbar.spec.ts (current SC#6 logged-in locator — brittle):
```typescript
await expect(
  page
    .locator('button[type="submit"], [role="button"]')
    .filter({ hasText: /Выйти|Sign out|Cerrar/ })
    .first(),
).toBeVisible({ timeout: 5_000 });
```

From landing/e2e/pricing.spec.ts (current SC#5 cookie assertion — context.cookies misses it):
```typescript
await expect
  .poll(async () => {
    const cookies = await page.context().cookies();
    return cookies.find((c) => c.name === "pricing_currency")?.value;
  }, { timeout: 10_000 })
  .toBe("USD");
```

From landing/playwright.config.ts (DO NOT CHANGE — NODE_ENV=development is intentional):
```typescript
"NODE_ENV=development " +   // Strict Mode in dev surfaces these bugs; production builds don't run Strict Mode.
"HOSTNAME=127.0.0.1 " +
"PORT=3000 " +
"node .next/standalone/server.js",
```

Strict Mode lifecycle reminder (React 18+/19): every effect runs `effect → cleanup → effect` on dev mounts to surface cleanup bugs. Module-level state and useEffect-body-scoped resets both survive this; useRef values DO survive but the cleanup runs between the two passes, so any ref the cleanup mutates is "poisoned" before the real mount's effect body sees it.
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Fix Strict Mode polling latch in poll-client.tsx (SC#4 happy + timeout / WEB-06)</name>
  <files>landing/src/app/[locale]/(app)/pay/success/poll-client.tsx</files>
  <read_first>
    - landing/src/app/[locale]/(app)/pay/success/poll-client.tsx (the file being modified — read in full to see the useEffect at lines 147-159 and the stop()/stopped.current relationship)
    - landing/e2e/pay-success.spec.ts (read both SC#4 happy at line 22 and SC#4 timeout at line 76 so the fix satisfies BOTH tests' acceptance shape — happy expects POST /api/v1/auth/refresh before active text; timeout expects takingLonger after 30s of pending)
    - .planning/phases/04-landing-surfaces/04-VERIFICATION.md (gaps section, root-cause A — confirms stopped.current persists across Strict Mode and that the recommended fix is a single-line reset)
    - .planning/phases/04-landing-surfaces/04-07-checkout-pay-success-fail-SUMMARY.md (D-21 polling parameters — INTERVAL_MS=2000, ESCALATE_AFTER_POLL=6, TIMEOUT_MS=30000 — DO NOT change these)
  </read_first>
  <action>
    Open `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx`. Locate the `useEffect` at line 147. Insert two reset statements as the FIRST and SECOND statements inside the useEffect body, BEFORE `pollOnce()`:

    Replace the body of the useEffect that currently reads:
    ```ts
    useEffect(() => {
      // Kick the first poll immediately so the user sees a transition at ~t=0
      // rather than waiting INTERVAL_MS for the first network call.
      pollOnce();
      timerRef.current = window.setInterval(pollOnce, INTERVAL_MS);
      timeoutRef.current = window.setTimeout(() => {
        if (stopped.current) return;
        if (timerRef.current !== null) window.clearInterval(timerRef.current);
        setView("takingLonger");
      }, TIMEOUT_MS);
      return () => stop();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    ```

    With:
    ```ts
    useEffect(() => {
      // Reset the one-shot lifecycle refs at the TOP of the effect body so
      // React Strict Mode's simulated unmount → remount cycle does not leave
      // stopped.current=true (set by the first pass's cleanup → stop()) and
      // strand pollOnce() in an early-return branch. Without these resets,
      // SC#4 happy and SC#4 timeout both fail under NODE_ENV=development.
      stopped.current = false;
      pollNo.current = 0;
      // Kick the first poll immediately so the user sees a transition at ~t=0
      // rather than waiting INTERVAL_MS for the first network call.
      pollOnce();
      timerRef.current = window.setInterval(pollOnce, INTERVAL_MS);
      timeoutRef.current = window.setTimeout(() => {
        if (stopped.current) return;
        if (timerRef.current !== null) window.clearInterval(timerRef.current);
        setView("takingLonger");
      }, TIMEOUT_MS);
      return () => stop();
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    ```

    Do NOT change anything else in the file. Do NOT change `INTERVAL_MS`, `ESCALATE_AFTER_POLL`, `TIMEOUT_MS`, the `stop()` function, the `pollOnce()` body, or the `refresh()` body. Do NOT change the `useRef` initialisation lines (the refs are still useful — only the cleanup-poisoning is the bug). The reset on remount restores the polling lifecycle cleanly; the cleanup-on-real-unmount continues to stop polling correctly because real unmounts do NOT re-run the effect body.
  </action>
  <verify>
    <automated>cd /Users/abdunabi/Desktop/vpn/landing && grep -nE "stopped\.current = false" src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx | grep -v "function stop" | head -3 && grep -nE "pollNo\.current = 0" src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx | head -3 && npx tsc --noEmit</automated>
  </verify>
  <acceptance_criteria>
    - `grep -c "stopped.current = false" landing/src/app/[locale]/(app)/pay/success/poll-client.tsx` returns at least 1 (the new reset line in useEffect body)
    - `grep -c "pollNo.current = 0" landing/src/app/[locale]/(app)/pay/success/poll-client.tsx` returns exactly 1 (the new reset line in useEffect body)
    - The two reset lines appear BEFORE the `pollOnce();` call on line 150 (verifiable via line-numbered grep: `grep -n "stopped.current = false\|pollNo.current = 0\|pollOnce()" .../poll-client.tsx` shows the resets at lower line numbers than `pollOnce()`)
    - `cd landing && npx tsc --noEmit` exits 0
    - The `stop()` function definition is unchanged (still sets `stopped.current = true`)
    - `INTERVAL_MS`, `ESCALATE_AFTER_POLL`, `TIMEOUT_MS` literal values are unchanged (still `2000`, `6`, `30000`)
  </acceptance_criteria>
  <done>
    poll-client.tsx is Strict-Mode-safe: the polling lifecycle resets on every effect mount (including Strict Mode's simulated remount), `pollOnce()` is allowed to run, and the cleanup still correctly halts polling on genuine unmounts. TypeScript still compiles.
  </done>
</task>

<task type="auto">
  <name>Task 2: Fix Strict Mode checkout latch in pricing-client.tsx using module-level Set guard (SC#2 / WEB-05)</name>
  <files>landing/src/app/[locale]/(app)/pricing/pricing-client.tsx</files>
  <read_first>
    - landing/src/app/[locale]/(app)/pricing/pricing-client.tsx (the file being modified — read in full to see the useEffect at lines 49-96 and the fired.current latch at line 46/52/54)
    - landing/e2e/pricing.spec.ts SC#2 test at line 75 (confirms the test asserts: POST /api/v1/checkout fires within 15s AND navigation to gate.lava.top is observed — both must work)
    - .planning/phases/04-landing-surfaces/04-VERIFICATION.md (root-cause A — explains why useRef-based latches break under Strict Mode and recommends a module-level Set keyed by checkout identity)
    - .planning/phases/04-landing-surfaces/04-07-checkout-pay-success-fail-SUMMARY.md ("Decisions" section — LAVA_URL_PATTERN regex MUST stay; 401 → /login bounce contract MUST stay)
  </read_first>
  <action>
    Open `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx`. Make exactly two edits — one to add the module-level Set, one to swap the `useRef` guard for a Set lookup.

    Edit 1 — Add the module-level Set immediately after the `LAVA_URL_PATTERN` declaration (line 41). The new block becomes:
    ```ts
    // Defence-in-depth whitelist for the backend-supplied payment_url. The backend
    // is trusted, but a single regex eliminates the open-redirect-via-payment-
    // provider surface entirely — see threat T-04-07-01.
    const LAVA_URL_PATTERN = /^https:\/\/(gate\.|app\.|pay\.)?lava\.top\//;

    // Module-level one-shot guard. Keyed by (plan, period, currency) so a user
    // who genuinely re-attempts a different plan/period/currency combination
    // gets a fresh checkout, but the same combination from a Strict Mode
    // simulated remount (NODE_ENV=development) does NOT fire a duplicate POST.
    //
    // Why module-level instead of useRef: React Strict Mode unmounts then
    // remounts every component in dev to surface cleanup bugs. A `useRef(false)`
    // local guard either (a) loses its value on the real mount if reset in the
    // effect body, defeating the latch, or (b) persists across mount/remount
    // but the cleanup poisons it, blocking the real mount's POST. A module-
    // level Set survives both passes and is checked AFTER the early-return
    // conditions so it only protects the actual POST.
    const inflightCheckouts = new Set<string>();
    ```

    Edit 2 — Replace the existing useEffect body (lines 49-96) `fired.current` guard with the Set check. Remove the `const fired = useRef(false);` line (line 46). The useEffect must look exactly like this after the edit:
    ```ts
    useEffect(() => {
      if (checkout !== "auto" || !plan || !period) return;
      // Strict-Mode-safe one-shot — module-level Set survives the simulated
      // unmount → remount cycle (NODE_ENV=development) AND prevents duplicate
      // POST across legitimate remounts of the same (plan, period, currency).
      const key = `${plan}|${period}|${currency}`;
      if (inflightCheckouts.has(key)) return;
      inflightCheckouts.add(key);
      (async () => {
        try {
          const r = await fetch("/api/v1/checkout", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            credentials: "same-origin",
            body: JSON.stringify({
              plan_code: plan,
              periodicity: period,
              currency,
            }),
          });
          if (r.status === 401) {
            // Session expired between page render and POST. Round-trip back
            // through /login preserving the same auto-checkout intent so the
            // flow resumes on return (CONTEXT D-19 hand-off).
            const next = `/pricing?plan=${plan}&period=${period}&currency=${currency}&checkout=auto`;
            router.replace(`/login?next=${encodeURIComponent(next)}`);
            return;
          }
          if (!r.ok) {
            setError(t("network"));
            return;
          }
          const json = await r.json().catch(() => null);
          const url = json?.payment_url;
          if (typeof url !== "string" || !LAVA_URL_PATTERN.test(url)) {
            // Backend returned a non-lava URL — defensive reject (T-04-07-01).
            setError(t("network"));
            return;
          }
          // Hard navigation to the payment provider — replaces the current
          // history entry so the back button returns to /pricing rather than
          // the transient "redirecting" state.
          window.location.href = url;
        } catch {
          // Network / parse failure. Show the i18n network error so the user
          // knows the click was registered and they can retry.
          setError(t("network"));
        }
      })();
    }, [checkout, plan, period, currency, router, t]);
    ```

    Also remove the now-unused `useRef` from the import. The import line should change from:
    ```ts
    import { useEffect, useRef, useState } from "react";
    ```
    to:
    ```ts
    import { useEffect, useState } from "react";
    ```

    Do NOT change the `Props` type, the `LAVA_URL_PATTERN` regex, the 401 redirect URL shape, the error-rendering JSX at lines 98-107, or the `useTranslations("errors")` / `useRouter()` calls. The Set is intentionally never cleared — for a single-page-app instance the keys are effectively per-load (page reloads create a fresh module instance). If a user wants to attempt the same plan/period/currency a second time after a failure, they can refresh the page.
  </action>
  <verify>
    <automated>cd /Users/abdunabi/Desktop/vpn/landing && grep -n "inflightCheckouts" src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx | head -5 && ! grep -n "fired.current" src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx && ! grep -nE "useRef|const fired" src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx && grep -n "LAVA_URL_PATTERN" src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx | head -2 && npx tsc --noEmit</automated>
  </verify>
  <acceptance_criteria>
    - `grep -c "inflightCheckouts" landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` returns at least 3 (declaration + .has + .add)
    - `grep -c "fired.current" landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` returns exactly 0 (old latch removed)
    - `grep -c "useRef" landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` returns exactly 0 (import no longer references useRef)
    - `grep -c "LAVA_URL_PATTERN" landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` returns exactly 2 (declaration + usage in the .test call) — regex MUST be preserved
    - `grep -E "router\.replace.*/login.*next" landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` matches exactly 1 line (401 bounce contract preserved)
    - The Set declaration `const inflightCheckouts = new Set<string>();` is at module scope (line < 50) — verifiable by `grep -nE "^const inflightCheckouts" .../pricing-client.tsx`
    - `cd landing && npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>
    pricing-client.tsx is Strict-Mode-safe: the module-level Set guards against duplicate POSTs across both the Strict Mode simulated remount AND legitimate remounts of the same (plan, period, currency). The 401 bounce, LAVA_URL_PATTERN check, and error-render JSX are unchanged. TypeScript compiles.
  </done>
</task>

<task type="auto">
  <name>Task 3: Add data-testid to UserMenu Sign-out button and switch navbar.spec.ts locator (SC#6 logged-in / WEB-09)</name>
  <files>landing/src/components/app/user-menu.tsx, landing/e2e/navbar.spec.ts</files>
  <read_first>
    - landing/src/components/app/user-menu.tsx (the file being modified — read in full to see the Popover.Portal → Positioner → Popup → form → button nesting at lines 55-77)
    - landing/e2e/navbar.spec.ts (read the SC#6 logged-in test at lines 28-69, especially the brittle locator at lines 63-68 which will be replaced)
    - .planning/phases/04-landing-surfaces/04-VERIFICATION.md (root-cause B — confirms the recommended fix is data-testid on the form submit button + getByTestId in the spec)
    - .planning/phases/04-landing-surfaces/04-02-app-shell-navbar-primitives-SUMMARY.md (UserMenu was created in Plan 02; the sign-out form contract is "submits to /api/auth/logout, works without JS" — preserve that)
  </read_first>
  <action>
    Edit 1 — `landing/src/components/app/user-menu.tsx`. Locate the `<button type="submit">` at lines 69-74 inside the `<form action="/api/auth/logout">`. Add a `data-testid="sign-out-button"` attribute. The element must look exactly like this after the edit:
    ```tsx
    <form action="/api/auth/logout" method="POST" className="pt-1">
      <button
        type="submit"
        data-testid="sign-out-button"
        className="w-full rounded-md px-3 py-2 text-left text-sm text-foreground transition hover:bg-muted"
      >
        {t("signOut")}
      </button>
    </form>
    ```
    Do NOT change anything else in user-menu.tsx — preserve the Popover.Root/Trigger/Portal/Positioner/Popup structure, the form action+method, the className, and the `{t("signOut")}` content.

    Edit 2 — `landing/e2e/navbar.spec.ts`. Replace the locator block at lines 63-68 with a Portal-aware getByTestId. The new block must be exactly:
    ```ts
      // Wait for the Popover's Portal content to mount before asserting the
      // testid — base-ui Popover renders into a Portal that is appended to
      // the body asynchronously after the trigger click. The data-testid on
      // the form submit button is the stable selector that survives any
      // future base-ui DOM-structure changes.
      await expect(
        page.getByTestId("sign-out-button"),
      ).toBeVisible({ timeout: 5_000 });
    ```
    Do NOT change anything else in navbar.spec.ts:
    - Preserve the `import { test, expect }` and `mockPlans, resetMockBackend` imports.
    - Preserve `test.beforeEach(async () => { await resetMockBackend(); });`.
    - Preserve the SC#6 logged-out test at lines 12-22 verbatim.
    - In the SC#6 logged-in test, preserve the `context.addCookies([{ name: "rv_at", ... }])` setup, the `mockPlans(page)` call, the `page.goto("/ru/pricing/")` navigation, the Pricing + Dashboard link assertions, the `page.getByRole("button", { name: /Account menu/i })` trigger lookup, the `trigger.click()` call, and the test title.
    - Only the trailing `await expect(page.locator(...).filter({ hasText: ... }).first()).toBeVisible(...)` block at lines 63-68 is replaced with the getByTestId assertion above.
  </action>
  <verify>
    <automated>cd /Users/abdunabi/Desktop/vpn/landing && grep -n 'data-testid="sign-out-button"' src/components/app/user-menu.tsx && grep -n 'getByTestId("sign-out-button")' e2e/navbar.spec.ts && ! grep -nE 'button\[type="submit"\], \[role="button"\]' e2e/navbar.spec.ts && npx tsc --noEmit</automated>
  </verify>
  <acceptance_criteria>
    - `grep -c 'data-testid="sign-out-button"' landing/src/components/app/user-menu.tsx` returns exactly 1
    - The `data-testid` attribute is on a `<button type="submit">` inside a `<form action="/api/auth/logout">` (verifiable by reading lines 68-75 of user-menu.tsx and seeing the form → button nesting preserved)
    - `grep -c 'getByTestId("sign-out-button")' landing/e2e/navbar.spec.ts` returns exactly 1
    - `grep -cE 'button\[type="submit"\], \[role="button"\]' landing/e2e/navbar.spec.ts` returns exactly 0 (old brittle locator removed)
    - SC#6 logged-out test (lines 12-22) is unchanged — verifiable by `grep -c "SC#6 logged-out" landing/e2e/navbar.spec.ts` returning 1 and the assertion shape unchanged
    - The Popover.Root → Trigger → Portal → Positioner → Popup structure in user-menu.tsx is unchanged — verifiable by `grep -cE "Popover\\.(Root|Trigger|Portal|Positioner|Popup)" landing/src/components/app/user-menu.tsx` returning at least 5
    - `cd landing && npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>
    UserMenu's Sign-out button has a stable test id that survives base-ui Portal DOM structure. The navbar SC#6 logged-in test uses a Portal-aware locator. The Sign-out form still posts to /api/auth/logout and still works without JavaScript (form action+method preserved). TypeScript compiles.
  </done>
</task>

<task type="auto">
  <name>Task 4: Switch SC#5 cookie assertion from context.cookies() to page.evaluate(document.cookie) (WEB-04 / SC#5 cookie)</name>
  <files>landing/e2e/pricing.spec.ts</files>
  <read_first>
    - landing/e2e/pricing.spec.ts (the file being modified — read in full to see the SC#5 cookie assertion at lines 55-60)
    - landing/src/components/app/currency-switcher.tsx (read in full to CONFIRM the cookie write is synchronous on click — line 58 `document.cookie = ...` runs BEFORE the `start()` transition at line 59, so the cookie IS written before the test could possibly observe it; this confirms the failure is in the OBSERVATION method, not the WRITE)
    - .planning/phases/04-landing-surfaces/04-VERIFICATION.md (root-cause C — recommends page.evaluate(() => document.cookie) as the lower-risk fix; the code is correct, the test was misreading)
    - .planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-SUMMARY.md (CurrencySwitcher cookie attributes: Path=/, SameSite=Lax, Max-Age=2592000, NOT HttpOnly — so document.cookie can read it)
  </read_first>
  <action>
    Open `landing/e2e/pricing.spec.ts`. Replace the cookie poll block at lines 55-60 with a `page.evaluate` poll that reads the cookie from the page's own document. The new block must be exactly:
    ```ts
      // Cookie write is synchronous in the click handler (CurrencySwitcher's
      // document.cookie assignment runs BEFORE the start() transition). Read
      // the cookie from the page's own document.cookie rather than via
      // page.context().cookies() — Playwright's CDP cookie API has been
      // observed to miss host-only cookies written via document.cookie from
      // a click handler within the 10s polling window, while document.cookie
      // reads always reflect the live jar from the page's perspective.
      await expect
        .poll(
          async () => {
            const raw = await page.evaluate(() => document.cookie);
            const match = raw
              .split(";")
              .map((p) => p.trim())
              .find((p) => p.startsWith("pricing_currency="));
            return match ? match.split("=")[1] : undefined;
          },
          { timeout: 10_000 },
        )
        .toBe("USD");
    ```

    Preserve everything else in the file:
    - Preserve all imports at the top.
    - Preserve `test.beforeEach(async () => { await resetMockBackend(); });`.
    - Preserve the test title `"SC#5: /pricing renders + currency switcher persists choice in cookie"`.
    - Preserve the `await mockPlans(page); await page.goto("/ru/pricing/", { waitUntil: "networkidle" });` setup.
    - Preserve the H1 + Pro visibility assertions at lines 28-31.
    - Preserve the `await page.waitForFunction(...)` hydration gate at lines 35-45.
    - Preserve the USD chip click at line 51.
    - Preserve the URL poll assertion at line 62 (this one already works: `await expect.poll(() => page.url(), { timeout: 10_000 }).toMatch(/currency=USD/);`).
    - Preserve the SC#2 test at lines 75-126 verbatim.
    - Preserve the SC#3 tests at lines 132-185 verbatim.

    Only the `expect.poll(...).toBe("USD")` block at lines 55-60 (the `context.cookies()` part) is replaced.
  </action>
  <verify>
    <automated>cd /Users/abdunabi/Desktop/vpn/landing && grep -n "page.evaluate(() => document.cookie)" e2e/pricing.spec.ts && grep -n 'pricing_currency=' e2e/pricing.spec.ts | head -3 && ! grep -nE "page\.context\(\)\.cookies\(\)" e2e/pricing.spec.ts && npx tsc --noEmit</automated>
  </verify>
  <acceptance_criteria>
    - `grep -c "page.evaluate(() => document.cookie)" landing/e2e/pricing.spec.ts` returns exactly 1
    - `grep -c 'startsWith("pricing_currency=")' landing/e2e/pricing.spec.ts` returns exactly 1
    - `grep -cE "page\\.context\\(\\)\\.cookies\\(\\)" landing/e2e/pricing.spec.ts` returns exactly 0 (old API removed from SC#5)
    - `grep -c "currency=USD" landing/e2e/pricing.spec.ts` returns at least 1 (URL poll assertion still in place)
    - SC#2 test title (line 75) is unchanged — verifiable by `grep -c "SC#2: ?checkout=auto fires POST /checkout" landing/e2e/pricing.spec.ts` returning 1
    - SC#3 test titles are unchanged — `grep -c "SC#3: logged-out PlanCard" landing/e2e/pricing.spec.ts` returns 1 and `grep -c "SC#3: /login carries next" landing/e2e/pricing.spec.ts` returns 1
    - `cd landing && npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>
    SC#5 cookie assertion reads `pricing_currency` from `document.cookie` directly, which reflects the live cookie jar from the page's own perspective. The CurrencySwitcher source code is unchanged because the cookie write was already correct — only the test's observation method needed fixing. TypeScript compiles.
  </done>
</task>

<task type="auto">
  <name>Task 5: Run the full Playwright suite end-to-end to confirm all 4 gap-closure fixes pass together</name>
  <files>(no source files modified — verification-only task)</files>
  <read_first>
    - landing/playwright.config.ts (confirm the two-server topology is unchanged — NODE_ENV=development MUST still be present; if it is not, Strict Mode wouldn't trigger and the fixes wouldn't be properly proven)
    - landing/e2e/_fixtures/run-mock-backend.cjs (familiarise with the mock endpoints in case any test surfaces an unexpected mock request — but no edits)
    - .planning/phases/04-landing-surfaces/04-08-deploy-smoke-tests-SUMMARY.md ("Running the smoke suite locally (pre-deploy)" section — confirms the exact pre-test build steps: npm run build, then cp -r .next/static .next/standalone/.next/static && cp -r public .next/standalone/public)
  </read_first>
  <action>
    Run the Phase 4 standalone-build refresh and the full Playwright suite. From `/Users/abdunabi/Desktop/vpn/landing`:

    1. Rebuild the standalone bundle so the source-code edits from Tasks 1-3 take effect at runtime:
       ```bash
       BACKEND_API_URL=http://127.0.0.1:4555 \
       REVALIDATE_SECRET=test-revalidate-secret \
       APPLE_SERVICE_ID=test.web \
       APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple \
       GOOGLE_CLIENT_ID_WEB=test.google \
       GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google \
       APP_URL=http://localhost:3000 \
       npm run build
       ```
       This MUST exit 0. The build is the only way the patched pricing-client.tsx, poll-client.tsx and user-menu.tsx land inside `.next/standalone/`.

    2. Copy the static assets the Playwright runner expects (per the 04-08 SUMMARY runbook):
       ```bash
       cp -r .next/static .next/standalone/.next/static
       cp -r public .next/standalone/public
       ```
       The `cp` will overwrite the previous copies — that's intentional.

    3. Run the full Playwright suite:
       ```bash
       npm run test:e2e
       ```
       Expected: 10 tests pass in ~37-60 seconds wall-clock. The four previously-failing tests must now all pass:
       - `pricing.spec.ts > SC#5: /pricing renders + currency switcher persists choice in cookie`
       - `pricing.spec.ts > SC#2: ?checkout=auto fires POST /checkout and triggers lava.top navigation`
       - `pay-success.spec.ts > SC#4 happy: polls → paid → force-refresh fires → 'Pro active' renders`
       - `pay-success.spec.ts > SC#4 timeout: pending forever → 'taking longer / we'll email you'`
       - `navbar.spec.ts > SC#6 logged-in: navbar shows Pricing + Dashboard + Sign-out (via avatar menu)`

       (Plus the 5 already-passing tests: SC#1 login render+CSRF, SC#5 URL part, SC#3 logged-out CTA, SC#3 hidden inputs, SC#6 logged-out.)

    If ANY test fails:
    - If `SC#2` still fails: re-read Task 2's pricing-client.tsx — verify the Set declaration is at module scope (not inside the component) and that `inflightCheckouts.has(key)` is checked AFTER the early-return for `checkout !== "auto"`.
    - If `SC#4 happy` or `SC#4 timeout` still fails: re-read Task 1's poll-client.tsx — verify `stopped.current = false; pollNo.current = 0;` are the FIRST two statements inside the useEffect body, BEFORE `pollOnce();`.
    - If `SC#5 cookie` still fails: re-read Task 4's pricing.spec.ts edit — verify the `page.evaluate(() => document.cookie)` is inside the `expect.poll` async function and that `pricing_currency=` prefix matching is correct.
    - If `SC#6 logged-in` still fails: re-read Task 3 — verify both files were edited (testid in user-menu.tsx AND getByTestId in navbar.spec.ts) and that the standalone build picked up the user-menu.tsx change (re-run step 1).

    Do NOT modify `landing/playwright.config.ts` to change NODE_ENV — that would mask the bug rather than fix it (per VERIFICATION.md and CONTEXT.md scope).
    Do NOT modify `landing/src/components/app/currency-switcher.tsx` — the code is correct; only the test needed fixing.
  </action>
  <verify>
    <automated>cd /Users/abdunabi/Desktop/vpn/landing && BACKEND_API_URL=http://127.0.0.1:4555 REVALIDATE_SECRET=test-revalidate-secret APPLE_SERVICE_ID=test.web APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple GOOGLE_CLIENT_ID_WEB=test.google GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google APP_URL=http://localhost:3000 npm run build && cp -r .next/static .next/standalone/.next/static && cp -r public .next/standalone/public && npm run test:e2e</automated>
  </verify>
  <acceptance_criteria>
    - `npm run build` exits 0 (the patched sources compile against Next.js 16 and TypeScript without errors)
    - `npm run test:e2e` exits 0 — all 10 tests in the suite pass
    - The terminal output includes the lines `pricing.spec.ts > SC#5: /pricing renders` PASSED (no `failed`/`timeout` line referencing this test)
    - The terminal output includes `pricing.spec.ts > SC#2: ?checkout=auto fires POST` PASSED
    - The terminal output includes `pay-success.spec.ts > SC#4 happy` PASSED
    - The terminal output includes `pay-success.spec.ts > SC#4 timeout` PASSED
    - The terminal output includes `navbar.spec.ts > SC#6 logged-in` PASSED
    - The terminal output shows `10 passed` (full count) — verifiable in the final test summary line
    - `grep -n "NODE_ENV=development" landing/playwright.config.ts` returns 1 line (unchanged — Strict Mode still active, fixes prove themselves under the hostile environment)
  </acceptance_criteria>
  <done>
    All 10 Playwright tests pass under NODE_ENV=development (React Strict Mode active). The 4 failing observable truths from 04-VERIFICATION.md (SC#2, SC#4 happy, SC#4 timeout, SC#5 cookie, SC#6 logged-in) are now all green. Phase 4 advances from 4/6 truths verified to 6/6 (SC#1 remains NEEDS-HUMAN, explicitly out of scope). Standalone build still produces a clean .next/standalone bundle.
  </done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| Browser → Next standalone (Node) | Cookie reads from `document.cookie` (CurrencySwitcher's pricing_currency); already-trusted browser→server proxy is unchanged |
| React component → React Strict Mode lifecycle | Cleanup poisoning of useRef latches — internal-only, no external attacker can trigger Strict Mode |
| Module-level state (inflightCheckouts Set) | Per-bundle, per-page-load — survives Strict Mode mount/remount but is recreated on every fresh page load (no cross-session contamination) |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-09-01 | Tampering | inflightCheckouts module-level Set | accept | Set is per-page-load; key is `${plan}|${period}|${currency}` derived from URL params; even if attacker sets ?plan=foo, backend re-validates plan_code per Phase 3 PAY-08. No new tampering surface vs the prior useRef latch (which had the same property). |
| T-04-09-02 | Repudiation | pricing-client.tsx checkout POST | accept | Already mitigated by Phase 3 — backend logs every /api/v1/checkout call with the authenticated user_id. The Set guard changes WHEN a POST fires, not WHO can fire it; no auth surface change. |
| T-04-09-03 | Information Disclosure | data-testid="sign-out-button" attribute | accept | data-testid is visible in the rendered HTML to anyone who views source — but it leaks zero secret data (just confirms there's a sign-out button, which is also literally visible as text). Standard E2E testing practice; no PII or token exposure. |
| T-04-09-04 | Information Disclosure | page.evaluate(() => document.cookie) in test | accept | document.cookie in test code reads ONLY cookies the page would already see (HttpOnly cookies remain invisible). pricing_currency is intentionally non-HttpOnly per 04-05 SUMMARY because the switcher needs to read its own previous choice. No HttpOnly token (rv_at/rv_rt/rv_user) is touched by this test code. |
| T-04-09-05 | Denial of Service | Strict-Mode reset in poll-client.tsx | accept | Resetting stopped.current=false + pollNo.current=0 at the top of the effect body restores the polling lifecycle but cannot cause runaway polling — the 30s TIMEOUT_MS is unchanged and the cleanup-on-real-unmount still runs `stop()`. Polling effective max remains 15 per page-load (TIMEOUT_MS / INTERVAL_MS). |
| T-04-09-06 | Elevation of Privilege | None | n/a | No auth-bearing code paths, no privilege escalation surfaces, no new endpoints, no new env vars. All four fixes are defensive (resolve race conditions / brittle locators / cookie observation method). The 401 → /login bounce contract in pricing-client.tsx is preserved verbatim. |

**Summary:** This plan introduces zero new attack surface. All five edits are defensive fixes for race conditions (Strict Mode latch bypass × 2), a brittle test locator (UserMenu Popover Portal), and a Playwright cookie-API observation quirk. The Pro launch security bar from CLAUDE.md (no Critical/High security regressions before any paying user) is honoured: nothing here weakens any existing mitigation — the Phase 3 PAY-08 server-side plan validation, Phase 2 HttpOnly cookie scope, T-04-07-01 LAVA_URL_PATTERN, T-04-04-01 OAuth state CSRF, and the rv_at/rv_rt cookie shape are all unchanged.
</threat_model>

<verification>
## Overall Phase 4 Verification Updates (re-run after this plan)

After the 5 tasks complete:

1. **Re-run the full E2E suite:**
   ```bash
   cd landing && npm run test:e2e
   # Expect: 10 passed (~37-60s)
   ```

2. **Confirm the four previously-failing truths now pass:**
   - SC#2 (pricing.spec.ts:75) — POST /api/v1/checkout fires + gate.lava.top navigation observed
   - SC#4 happy (pay-success.spec.ts:22) — POST /api/v1/auth/refresh fires + "Pro is active!" text visible
   - SC#4 timeout (pay-success.spec.ts:76) — takingLonger UI renders after 30s
   - SC#5 cookie (pricing.spec.ts:21 cookie part) — pricing_currency=USD readable from document.cookie within 10s
   - SC#6 logged-in (navbar.spec.ts:28) — Sign-out button visible via getByTestId within 5s

3. **Confirm no regressions:**
   - SC#1 login render + CSRF tests still pass
   - SC#3 logged-out CTA + hidden inputs tests still pass
   - SC#5 URL part still passes
   - SC#6 logged-out test still passes

4. **Confirm the production build still works:**
   ```bash
   cd landing && npm run build
   # Expect: exit 0, .next/standalone/ produced cleanly
   ```

5. **Confirm Strict Mode is still active (the fix proves itself under the hostile environment):**
   ```bash
   grep -n "NODE_ENV=development" landing/playwright.config.ts
   # Expect: 1 line — Strict Mode still triggers in the test runner, fixes still pass
   ```

After this plan, Phase 4 score advances from 4/6 truths verified to 6/6 (with SC#1 still NEEDS-HUMAN for live Apple/Google credentials — explicitly out of scope for this plan). The phase is then ready for the human verification steps in 04-VERIFICATION.md ("Human Verification Required" sections 1-4: Apple/Google live OAuth + lava.top sandbox full flow).
</verification>

<success_criteria>
Plan is complete when:

- [ ] Task 1: `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx` contains `stopped.current = false;` and `pollNo.current = 0;` as the first two statements inside the useEffect body, BEFORE `pollOnce();`. No other changes to the file.
- [ ] Task 2: `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` contains a module-level `const inflightCheckouts = new Set<string>();` declaration AFTER `LAVA_URL_PATTERN`. The useEffect guards on `inflightCheckouts.has(key)` instead of `fired.current`. `useRef` is removed from the imports.
- [ ] Task 3: `landing/src/components/app/user-menu.tsx` `<button type="submit">` carries `data-testid="sign-out-button"`. `landing/e2e/navbar.spec.ts` SC#6 logged-in test uses `page.getByTestId("sign-out-button")` for the assertion. Old `button[type="submit"], [role="button"]` locator is removed.
- [ ] Task 4: `landing/e2e/pricing.spec.ts` SC#5 cookie assertion uses `page.evaluate(() => document.cookie)` with a `pricing_currency=` prefix parse. Old `page.context().cookies()` call in SC#5 is removed.
- [ ] Task 5: `cd landing && npm run build && npm run test:e2e` exits 0 with 10/10 tests passing.
- [ ] `landing/playwright.config.ts` `NODE_ENV=development` setting is UNCHANGED (would mask the bug rather than fix it).
- [ ] `landing/src/components/app/currency-switcher.tsx` is UNCHANGED (test-side fix per VERIFICATION.md root-cause C recommendation).
- [ ] All 5 task-level `npx tsc --noEmit` checks exit 0 (no TypeScript regressions).
- [ ] Phase 4 4/6 → 6/6 truths verified (SC#1 still NEEDS-HUMAN, intentionally out of scope).
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-09-SUMMARY.md` documenting:

- The 5 file edits made (3 source + 2 test) with commit SHAs
- E2E suite final state: 10/10 passing (was 5/10)
- Root-cause taxonomy: 3 Strict Mode latch bypasses (1 single-line reset, 1 module-level Set refactor, 0 in CurrencySwitcher because code was correct) + 1 brittle Playwright Popover Portal locator (data-testid + getByTestId)
- Confirmation that NODE_ENV=development in playwright.config.ts is UNCHANGED (per scope guard)
- Confirmation that currency-switcher.tsx is UNCHANGED (per VERIFICATION.md test-side recommendation)
- Pre/post truth count: 4/6 → 6/6 verified; SC#1 still NEEDS-HUMAN
- Threat closure: T-04-09-01..06 all `accept` or `n/a` (zero new attack surface)
</output>
