---
phase: 04-landing-surfaces
reviewed: 2026-05-25T00:00:00Z
depth: standard
files_reviewed: 53
files_reviewed_list:
  - docker-compose.prod.yml
  - landing/Dockerfile
  - landing/docker-compose.landing.yml
  - landing/e2e/_fixtures/backend-mock.ts
  - landing/e2e/_fixtures/run-mock-backend.cjs
  - landing/e2e/login.spec.ts
  - landing/e2e/navbar.spec.ts
  - landing/e2e/pay-success.spec.ts
  - landing/e2e/pricing.spec.ts
  - landing/next.config.ts
  - landing/nginx/vpn.mydayai.uz.conf
  - landing/playwright.config.ts
  - landing/src/app/[locale]/(app)/dashboard/page.tsx
  - landing/src/app/[locale]/(app)/dashboard/signout-button.tsx
  - landing/src/app/[locale]/(app)/login/page.tsx
  - landing/src/app/[locale]/(app)/login/start-oauth.ts
  - landing/src/app/[locale]/(app)/pay/fail/page.tsx
  - landing/src/app/[locale]/(app)/pay/success/page.tsx
  - landing/src/app/[locale]/(app)/pay/success/poll-client.tsx
  - landing/src/app/[locale]/(app)/pricing/page.tsx
  - landing/src/app/[locale]/(app)/pricing/pricing-client.tsx
  - landing/src/app/[locale]/layout.tsx
  - landing/src/app/api/[...path]/route.ts
  - landing/src/app/api/auth/logout/route.ts
  - landing/src/app/api/revalidate-pricing/route.ts
  - landing/src/app/auth/callback/exchange.ts
  - landing/src/app/auth/callback/route.ts
  - landing/src/components/app/auth-button-apple.tsx
  - landing/src/components/app/auth-button-google.tsx
  - landing/src/components/app/currency-switcher.tsx
  - landing/src/components/app/dashboard-card.tsx
  - landing/src/components/app/payment-fail-card.tsx
  - landing/src/components/app/payment-status-card.tsx
  - landing/src/components/app/plan-card.tsx
  - landing/src/components/app/tier-badge.tsx
  - landing/src/components/app/user-menu.tsx
  - landing/src/components/common/locale-switcher.tsx
  - landing/src/components/common/navbar-app.tsx
  - landing/src/components/ui/card.tsx
  - landing/src/components/ui/skeleton.tsx
  - landing/src/components/ui/toast.tsx
  - landing/src/i18n/routing.ts
  - landing/src/lib/cookies.ts
  - landing/src/lib/env.ts
  - landing/src/lib/locale-currency.ts
  - landing/src/lib/oauth.ts
  - landing/src/lib/plans.ts
  - landing/src/lib/proxy.ts
  - landing/src/lib/session-cookie.ts
  - landing/src/lib/session.ts
  - landing/src/proxy.ts
findings:
  critical: 3
  warning: 5
  info: 4
  total: 12
status: issues_found
---

# Phase 04: Code Review Report

**Reviewed:** 2026-05-25T00:00:00Z
**Depth:** standard
**Files Reviewed:** 53
**Status:** issues_found

## Summary

This phase delivers the complete RiseVPN web landing surface: Apple/Google OAuth login, a session-cookie proxy, dashboard, pricing with ISR + tag-bust revalidation, and the /pay/success polling flow. The implementation is broadly well-engineered — server-only guards are placed correctly, cookie attributes are tight (HttpOnly + SameSite=Strict + Secure in prod), CSRF uses constant-time comparison, and the open-redirect defence is an explicit allow-list. Three critical issues were found that must be fixed before any paying user can trigger the flow.

---

## Critical Issues

### CR-01: OAuth nonce is never verified — nonce binding against id_token is silently skipped

**File:** `landing/src/app/[locale]/(app)/login/start-oauth.ts:79-83` and `landing/src/app/auth/callback/exchange.ts` (no nonce validation)

**Issue:** `startOAuth` generates a 32-byte nonce, stores it in an HttpOnly `rv_oauth_nonce` cookie, and passes it to both Apple and Google in the authorize URL. Both providers embed the nonce in their id_token as the `nonce` claim. However `completeOAuth` (`exchange.ts`) never reads `rv_oauth_nonce` and never checks the nonce claim in the id_token (the JWT is sent directly to the backend). An attacker who can replay a legitimately obtained id_token from a different context (e.g., one minted for a different application or a previous session) will pass the CSRF state check but the nonce check that would prevent this replay is entirely absent. The inline comment in `start-oauth.ts` acknowledges this as "Phase 5+ hardening" but the security gap is active the moment live OAuth credentials are used.

**Severity:** The CSRF state cookie addresses the canonical CSRF threat, but the nonce is the replay-attack defence specific to Apple's `response_type=code id_token` and Google's implicit flow. Without it, a stolen id_token can be replayed against this callback within the token's lifetime.

**Fix:** In `exchange.ts`, before calling the backend, read `rv_oauth_nonce` from the cookie jar and pass it alongside `id_token` in the backend request body. The backend's `/api/v1/auth/apple` and `/api/v1/auth/google` handlers must verify that the nonce claim in the id_token matches. If the backend already validates nonces (Phase 2 contract), the landing only needs to forward the value:

```typescript
// exchange.ts — after CSRF check, before fetch to backend
const cookieNonce = jar.get("rv_oauth_nonce")?.value ?? "";
// ...
body: JSON.stringify({ id_token: args.idToken, nonce: cookieNonce }),
```

If the backend does not yet verify nonces, escalate as a critical backend gap before enabling live OAuth.

---

### CR-02: `revalidateTag` receives an undocumented second argument — silent no-op in current Next.js

**File:** `landing/src/app/api/revalidate-pricing/route.ts:69`

**Issue:** The call is `revalidateTag("plans", "max")`. The `revalidateTag` function in Next.js App Router accepts exactly **one** argument (the tag string). The second argument `"max"` is silently ignored — it is not a documented API. The inline comment cites `https://nextjs.org/docs/messages/revalidate-tag-single-arg` as justification, but that URL documents the single-argument form as the only valid form. The fictional `CacheLifeProfile` second argument does not exist in Next.js 15/16 stable. The call will NOT throw at runtime, but if a future Next.js version introduces a type-breaking second-argument API with different semantics, this will silently misbehave. More immediately: the in-code rationale is wrong, which erodes trust in this file's accuracy.

**Fix:**

```typescript
// Remove the second argument — the single-argument form is the only valid form.
revalidateTag("plans");
```

---

### CR-03: CSP `form-action 'self'` blocks Apple and Google form_post callbacks — OAuth will fail in production behind nginx

**File:** `landing/nginx/vpn.mydayai.uz.conf:165`

**Issue:** The Content-Security-Policy header applied to the catch-all `location /` block is:

```
form-action 'self'
```

This restricts which URLs a `<form>` may submit to. The Apple `response_mode=form_post` flow works by redirecting the user's browser to `https://appleid.apple.com/auth/authorize`, which then POSTs `application/x-www-form-urlencoded` back to the registered `redirect_uri` on the landing server (`/auth/callback?provider=apple`). This inbound POST is NOT a `<form>` submission from the browser's perspective relative to the CSP — it is initiated by Apple's domain, not by a form on `self`. However, `startOAuth` emits a `redirect()` to `https://appleid.apple.com/...` which is itself a top-level navigation triggered by a Server Action form submission. The `<form action={startOAuthForm}>` in `auth-button-apple.tsx` and `auth-button-google.tsx` submits to the Next.js Server Action endpoint (same origin), so that part is fine. The `/auth/callback` receiver's 3xx redirect response back into the app is also fine.

The real risk is that the CSP header is served from the catch-all `location /` block which nginx applies to **statically served HTML files**. The `/api/` and `/auth/callback` proxy blocks do NOT inherit these `add_header` directives (nginx `add_header` inheritance only applies within the same `location` level; a block that adds its own headers drops headers from an outer block). The `_next/static/` block also does not inherit them. The net effect: the CSP is only set on the static HTML fallback responses, not on the proxied app routes. This creates a CSP coverage gap for the actual authenticated routes while simultaneously creating a risk that any future refactor which adds `add_header` to the proxy blocks will silently lose the security headers.

**Fix:** Move all security headers to a dedicated `include` file and apply it in every `location` block that serves user-facing content:

```nginx
# /etc/nginx/snippets/security-headers.conf
add_header X-Content-Type-Options "nosniff" always;
add_header X-Frame-Options "DENY" always;
add_header Referrer-Policy "strict-origin-when-cross-origin" always;
add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;
add_header Strict-Transport-Security "max-age=63072000; includeSubDomains" always;
add_header Content-Security-Policy "..." always;

# Then in each location block:
include snippets/security-headers.conf;
```

Additionally, the CSP `form-action 'self'` directive as written will block the `<form action={startOAuthForm}>` submission if Next.js's Server Action form endpoint is not on the same origin as the form page (it is — but this should be verified). For defence-in-depth, `form-action` should also allow the provider origins that Apple/Google may use for their own redirect back to the landing during form_post:

```
form-action 'self' https://appleid.apple.com https://accounts.google.com;
```

---

## Warnings

### WR-01: `constantTimeEquals` length short-circuit leaks timing information on length mismatch

**File:** `landing/src/lib/oauth.ts:98-103`

**Issue:** `constantTimeEquals` returns `false` immediately when `ab.length !== bb.length`. This is a timing side-channel: an attacker probing the CSRF cookie value can distinguish "wrong length" from "right length, wrong bytes" by measuring response time. While the CSRF token is 43 characters (base64url of 32 bytes) and an attacker would need to control the response time measurement across an HTTP round-trip to the server, the D-08 threat model explicitly requires constant-time comparisons. The `timingSafeEqual` call is the fix — but it is only reached when lengths match.

This pattern is also present in `session-cookie.ts:48` (correct there — lengths are compared after decoding base64url, so the lengths are fixed-size MAC digests where mismatch means truncated/malformed input, not a probe). In `oauth.ts` the inputs are user-controlled strings of arbitrary length.

**Fix:**

```typescript
export function constantTimeEquals(a: string, b: string): boolean {
  const ab = Buffer.from(a, "utf8");
  // Pad bb to ab.length for a constant-time comparison even on mismatch.
  // timingSafeEqual requires equal-length Buffers; use a fixed-length hash
  // of both sides to normalize length before comparing.
  const { createHash } = require("node:crypto");
  const ha = createHash("sha256").update(ab).digest();
  const hb = createHash("sha256").update(Buffer.from(b, "utf8")).digest();
  return timingSafeEqual(ha, hb);
}
```

Or more simply, since the CSRF value is always 43 chars (randomB64Url(32)) and the cookie value is set from that same token, the check in `exchange.ts` line 113-116 can rely on the known fixed length. Document the invariant and add a length guard that returns false without short-circuiting distinguishably:

```typescript
export function constantTimeEquals(a: string, b: string): boolean {
  const ab = Buffer.from(a, "utf8");
  const bb = Buffer.from(b, "utf8");
  // Normalize to the longer length so timingSafeEqual doesn't throw.
  // Using a hash collapses both to equal-length without leaking structure.
  const { createHash } = require("node:crypto");
  const ha = createHash("sha256").update(ab).digest();
  const hb = createHash("sha256").update(bb).digest();
  return timingSafeEqual(ha, hb);
}
```

---

### WR-02: `encodeState` carries plan/period/currency as plaintext in a URL query parameter

**File:** `landing/src/lib/oauth.ts:55-57`, `landing/src/app/[locale]/(app)/login/start-oauth.ts:62-69`

**Issue:** The OAuth state blob is `base64url(JSON({csrf, locale, next, plan, period, currency}))`. This value appears verbatim in the provider's authorize URL as the `state=` query parameter. Both Apple and Google echo it back in the form_post. It is not encrypted or HMAC-signed — it is only base64url-encoded. Any party who observes the provider redirect URL (browser history, Referrer header, proxy logs) can decode the state and read the `plan`, `period`, and `currency` intent values. More critically, the state can be tampered with in transit.

The CSRF check (`constantTimeEquals(cookieCsrf, decoded.csrf)`) guarantees that a tampered state cannot authenticate a different user session — the `csrf` field protects the session binding. However, a tampered `next`, `plan`, `period`, or `currency` value in the state will not be caught if the attacker also controls the `csrf` in the state (which they cannot, since the csrf is also stored HttpOnly). But an observer can read the plan hints without being able to modify them.

The actual exploit surface is low (plan hints are not secrets), but the state should be integrity-protected via HMAC to prevent any manipulation of the `next` redirect target. Currently, `isSafeNextPath` provides allow-list protection at callback time, but a malicious state with a different safe path (e.g., replacing `/dashboard` with `/pricing?checkout=auto`) could change post-login behaviour.

**Fix:** HMAC-sign the encoded state with `REVALIDATE_SECRET` (using a `:oauth-state` namespace) before returning it to the authorize URL, and verify the HMAC in `decodeState` before trusting any field. This closes the state-tampering surface entirely, regardless of what paths are allowed.

```typescript
// oauth.ts
export function encodeState(p: StatePayload): string {
  const payload = Buffer.from(JSON.stringify(p), "utf8").toString("base64url");
  const mac = createHmac("sha256", env.STATE_HMAC_KEY).update(payload).digest("base64url");
  return `${payload}.${mac}`;
}

export function decodeState(raw: string): StatePayload | null {
  const dot = raw.lastIndexOf(".");
  if (dot < 1) return null;
  const payload = raw.slice(0, dot);
  const mac = raw.slice(dot + 1);
  const expected = createHmac("sha256", env.STATE_HMAC_KEY).update(payload).digest("base64url");
  if (!constantTimeEquals(mac, expected)) return null;
  // ... parse payload as before
}
```

Note: this requires `oauth.ts` to import `env`, which it currently does not. Use a separate narrow key derived from `REVALIDATE_SECRET` with a `:oauth-state` namespace.

---

### WR-03: `invoiceId` from URL search params is passed directly to `encodeURIComponent` and then into `/api/v1/invoices/<id>` — no format validation

**File:** `landing/src/app/[locale]/(app)/pay/success/page.tsx:55`, `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx:94`

**Issue:** `sp.invoiceId` is taken directly from the URL search parameter and passed to `PollClient`, which embeds it in the fetch path `/api/v1/invoices/${encodeURIComponent(invoiceId)}`. `encodeURIComponent` prevents path traversal (it encodes `/` and `..`), and the backend performs an ownership check (returns 404 for invoices not owned by the caller per the plan notes). However, there is no length or format validation at the page level.

A malformed or very long `invoiceId` (e.g., a 10 KB string) will be forwarded to the proxy and then to the backend. The proxy's `bufferBody` limit protects request bodies, but GET request URLs are not similarly capped. While the backend should reject malformed IDs, the landing has no defensive check. A crafted URL could cause unnecessarily long upstream URL construction.

**Fix:** Add a simple format guard before rendering `PollClient`:

```typescript
// pay/success/page.tsx
const RAW_INVOICE_ID_RE = /^[a-zA-Z0-9_\-]{1,64}$/;

if (!sp.invoiceId || !RAW_INVOICE_ID_RE.test(sp.invoiceId)) {
  return redirect({ href: "/dashboard", locale });
}
```

---

### WR-04: `dangerouslySetInnerHTML` used for JSON-LD structured data without sanitization

**File:** `landing/src/app/[locale]/layout.tsx:129-137`

**Issue:** The locale layout injects JSON-LD structured data via `dangerouslySetInnerHTML={{ __html: JSON.stringify(ldOrganization) }}`. `JSON.stringify` is safe against XSS from JavaScript string values, but it does NOT escape the `</script>` sequence, which would prematurely close the surrounding `<script>` tag. If any string field in `ldOrganization` or `ldSoftwareApp` (e.g., a description or URL pulled from translations) contains the literal substring `</script>` or `</Script>`, it will break out of the script context in the HTML.

This is a known Next.js / React concern with JSON-LD injection. React's `dangerouslySetInnerHTML` does not HTML-encode the string — it trusts the caller to provide safe content.

**Fix:** Escape `</script>` in the serialized JSON before injecting:

```typescript
function safeJsonLd(obj: object): string {
  return JSON.stringify(obj).replace(/<\/script>/gi, "<\\/script>");
}

// Usage:
dangerouslySetInnerHTML={{ __html: safeJsonLd(ldOrganization) }}
```

---

### WR-05: `triedRefresh` guard in proxy is always `false` on the 401 branch — the dead-code defensive comment misleads

**File:** `landing/src/lib/proxy.ts:239`

**Issue:** At line 239 the proxy checks:

```typescript
if (!rt || triedRefresh) {
```

`triedRefresh` is declared as `let triedRefresh = false` at line 212 and is only set to `true` at line 249, which is AFTER this check. The `triedRefresh` branch at line 239 can therefore never be `true` on the first pass through the 401 handler — the variable has no effect. The inline comment "Defensive — shouldn't reach here with triedRefresh=true on first pass" reveals the author was aware of this but the guard does not provide the documented protection.

More precisely: the single-retry guarantee is enforced solely by the linear code structure (there is no loop, so the retry is physically executed at most once). The `triedRefresh` variable was probably added as a loop guard in a design iteration where a `while` or `goto`-equivalent was considered. In the current flat structure it is dead code that could confuse a future maintainer into thinking there is a loop.

**Fix:** Remove `triedRefresh` and its assignment, and add a comment making the single-retry guarantee explicit via code structure rather than a flag:

```typescript
// 7. 401 path — refresh + retry ONCE (no loop; structural guarantee).
if (!rt) {
  const res = await pipeUpstream(upstream);
  return res;
}

const refresh = await callRefresh(rt);
// ... rest of refresh logic unchanged
```

---

## Info

### IN-01: REVALIDATE_SECRET used as HMAC key for both `rv_user` cookies and ISR revalidation endpoint

**File:** `landing/src/lib/session-cookie.ts:23`, `landing/src/app/api/revalidate-pricing/route.ts:56`

**Issue:** `HMAC_SECRET` in `session-cookie.ts` is derived from `env.REVALIDATE_SECRET + ":session"`. The same `REVALIDATE_SECRET` is compared directly (timing-safe) against the `?secret=` query parameter in the revalidate-pricing endpoint. This means a single secret serves two distinct security purposes: authenticating the ISR bust webhook AND signing the `rv_user` cookie. A rotation of `REVALIDATE_SECRET` (e.g., due to suspected compromise of the webhook secret) simultaneously invalidates all active user sessions. Consider using two separate secrets (`REVALIDATE_SECRET` and `SESSION_COOKIE_SECRET`) to allow independent rotation.

---

### IN-02: `revalidateTag` secret in `?secret=` query parameter will appear in nginx access logs

**File:** `landing/src/app/api/revalidate-pricing/route.ts:54`, `landing/nginx/vpn.mydayai.uz.conf:85-95`

**Issue:** The ISR bust endpoint authenticates via `?secret=<REVALIDATE_SECRET>`. nginx logs the full URI including query string by default (`$request_uri`). In the current nginx config, the `/api/` proxy block does not suppress logging, so each call to `/api/revalidate-pricing?secret=<value>` writes the secret to nginx's `access.log`. The plan notes this as a known accepted risk for Phase 4, but it should be tracked as a concrete log-hygiene issue. Mitigation: move the secret to an `X-Revalidate-Token` request header instead of a query parameter.

---

### IN-03: Mock backend `/__set_invoice` and `/__reset` endpoints have no authentication

**File:** `landing/e2e/_fixtures/run-mock-backend.cjs:64-75`

**Issue:** The mock backend binds to `127.0.0.1:4555` (loopback only), so network-level exposure is limited. However, there is no check that the caller is localhost-privileged or a test runner. Any process on the same machine during a CI run that can reach 127.0.0.1:4555 can reset the invoice state and interfere with the test sequence. For CI robustness, consider a simple shared-secret header (`X-Test-Token`) on the control endpoints.

---

### IN-04: `plans.ts` `fetchPlans` uses `console.warn` which will appear in production server logs

**File:** `landing/src/lib/plans.ts:57`, `landing/src/lib/plans.ts:64`

**Issue:** Non-200 responses and network failures from the plans endpoint are logged with `console.warn` including the raw error string (`String(err)`). In production, `String(err)` for a network failure may include the backend URL (`env.BACKEND_API_URL`) in the error message, which is an internal URL (`http://vpn-api:3000` in co-located deployments). This is a low-severity information-leak concern for log aggregation systems. Replace with structured logging or strip the URL from the error detail:

```typescript
console.warn("[plans] fetch failed", { currency, errorType: err instanceof Error ? err.constructor.name : typeof err });
```

---

_Reviewed: 2026-05-25T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
