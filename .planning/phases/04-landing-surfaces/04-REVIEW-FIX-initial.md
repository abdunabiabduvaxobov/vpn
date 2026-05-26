---
phase: 04-landing-surfaces
fixed_at: 2026-05-25T00:00:00Z
review_path: .planning/phases/04-landing-surfaces/04-REVIEW.md
iteration: 1
findings_in_scope: 8
fixed: 7
skipped: 1
status: partial
---

# Phase 04: Code Review Fix Report

**Fixed at:** 2026-05-25T00:00:00Z
**Source review:** `.planning/phases/04-landing-surfaces/04-REVIEW.md`
**Iteration:** 1

**Summary:**
- Findings in scope (Critical + Warning): 8
- Fixed: 7
- Skipped: 1 (CR-02 — fix suggestion would break the build under the actual installed Next.js version)

All 7 applied fixes pass `npx tsc --noEmit` against the existing project type-checker invocation pattern used in plan acceptance criteria. Each fix was committed atomically. Info findings (IN-01 through IN-04) were intentionally out of scope per the orchestrator brief.

## Fixed Issues

### WR-01: `constantTimeEquals` length short-circuit timing leak

**Files modified:** `landing/src/lib/oauth.ts`
**Commit:** `20389c4`
**Applied fix:** Replaced the `if (ab.length !== bb.length) return false; return timingSafeEqual(ab, bb);` shape with a SHA-256-hash-then-compare. Both inputs are now reduced to fixed-length 32-byte digests before `timingSafeEqual`, so the comparison runtime is independent of input length and content. The doc-comment was updated to call out the WR-01 closure and the SHA-256 trade-off (collisions computationally infeasible for any realistic input pair).

### WR-02: HMAC-sign OAuth state payload

**Files modified:** `landing/src/lib/oauth.ts`
**Commit:** `ed38b16`
**Applied fix:** Introduced a narrow HMAC key derived from `REVALIDATE_SECRET + ":oauth-state"` (namespace pattern mirrors `session-cookie.ts`'s `":session"` key). `encodeState` now emits `<base64url(JSON)>.<base64url(hmac)>`. `decodeState` splits on the LAST dot, recomputes the expected MAC, and rejects with `null` on length mismatch OR a `timingSafeEqual` failure. State tampering of any field (`next`, `plan`, `period`, `currency`, `locale`, `csrf`) is now caught before any cookie or backend operation.

### CR-01: OAuth id_token nonce verification

**Files modified:** `landing/src/lib/oauth.ts`, `landing/src/app/auth/callback/exchange.ts`
**Commit:** `1e5f891`
**Applied fix:**
- Added `decodeNonceFromIdToken(idToken)` to `oauth.ts`. Same trust model as `decodePlanIdFromJwt` — signature verification stays on the backend; the landing extracts the `nonce` claim for a fast-fail edge check.
- `completeOAuth` now reads `rv_oauth_nonce` from the cookie jar (alongside `rv_oauth_state`), decodes the id_token's nonce claim, and constant-time-compares the two BEFORE the backend call. Mismatch redirects to `/<locale>/login?error=oauth_state`.
- The nonce cookie value is also forwarded to the backend in the exchange request body (`{ id_token, nonce }`) so the backend's OIDC verifier can perform end-to-end nonce binding (AUTH-01 contract).
- Threat coverage extended for both Apple and Google providers (same code path serves both).

### CR-03: nginx security header inheritance + CSP form-action provider origins

**Files modified:** `landing/nginx/vpn.mydayai.uz.conf`
**Commit:** `9fe2ad1`
**Applied fix:**
- Added the full security-header set (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, `Strict-Transport-Security`, `Content-Security-Policy`) at SERVER scope so the proxied app routes (`/api/`, `/auth/callback`, `/login`, `/dashboard`, `/pricing`, `/pay/*`) — which previously declared no `add_header` of their own — now inherit them.
- Re-emitted `X-Content-Type-Options` and HSTS in every static-asset location block (`/_next/static/`, image MIME, `/icon`, `/apple-icon`, opengraph) so nginx's inheritance-shadowing rule (any local `add_header` drops the outer set) does not silently strip the security headers from those responses.
- Kept the full security-header set in `location /` (static HTML fallback) for the same shadowing reason — it has its own `Cache-Control` directive.
- Updated the CSP `form-action` directive from `'self'` to `'self' https://appleid.apple.com https://accounts.google.com` so Apple/Google's `response_mode=form_post` POST back to `/auth/callback` is not blocked by CSP.

### WR-03: invoiceId shape validation before upstream proxy call

**Files modified:** `landing/src/app/[locale]/(app)/pay/success/page.tsx`
**Commit:** `3ddb93c`
**Applied fix:** Added a `RAW_INVOICE_ID_RE = /^[A-Za-z0-9_-]{1,64}$/` allowlist and gated `<PollClient/>` rendering on it. Junk / over-long invoice IDs now redirect to `/pay/fail?reason=default` instead of being embedded in an upstream `/api/v1/invoices/<id>` URL. The regex is a superset of canonical UUID and length-capped so neither path-traversal probes nor 10 KB payload spam can reach the proxy.

### WR-04: JSON-LD `</script>` and U+2028/U+2029 escape

**Files modified:** `landing/src/app/[locale]/layout.tsx`
**Commit:** `3f4543d`
**Applied fix:** Added a `safeJsonLd(obj)` serializer that escapes:
- `<` → `<` (catches `</script>`, `</Script>`, `<!--`, and `<![CDATA[`)
- U+2028 / U+2029 line separators → escape sequences (legacy JS parser hazard)

Used via `dangerouslySetInnerHTML={{ __html: safeJsonLd(ldOrganization) }}` for both the Organization and SoftwareApplication JSON-LD blobs in the locale layout.

Implementation detail: the U+2028 / U+2029 regexes are built via the `new RegExp("<U+2028>", "g")` constructor form rather than as regex literals, because the TypeScript lexer treats literal U+2028 / U+2029 inside a `/.../` regex literal as "Unterminated regular expression literal" (TS1161).

### WR-05: Remove dead `triedRefresh` flag in proxy

**Files modified:** `landing/src/lib/proxy.ts`
**Commit:** `4b529c0`
**Applied fix:** Removed `let triedRefresh = false`, the `if (!rt || triedRefresh)` branch's `triedRefresh` predicate, and the `triedRefresh = true;` assignment. The dead "Defensive — shouldn't reach here with triedRefresh=true on first pass" cookie-clear branch was also removed (it was unreachable). Replaced with an explicit comment documenting that single-retry semantics come from the linear code structure (no loop), so a future maintainer cannot mistake a flag-driven retry for a loop guard.

## Skipped Issues

### CR-02: `revalidateTag("plans", "max")` second argument

**File:** `landing/src/app/api/revalidate-pricing/route.ts:69`
**Reason:** The reviewer's fix suggestion — `revalidateTag("plans")` (single-arg) — would actively break the build under the installed Next.js version (`16.2.4`, per `landing/package.json` and `node_modules/next/package.json`).

Verified against the installed Next.js source:

```
$ cat landing/node_modules/next/dist/server/web/spec-extension/revalidate.d.ts
export declare function revalidateTag(tag: string, profile: string | CacheLifeConfig): undefined;
```

And the runtime source:

```js
export function revalidateTag(tag, profile) {
  if (!profile) {
    console.warn(
      '"revalidateTag" without the second argument is now deprecated, add second argument of "max" or use "updateTag". See more info here: https://nextjs.org/docs/messages/revalidate-tag-single-arg'
    )
  }
  return revalidate([tag], `revalidateTag ${tag}`, profile)
}
```

Confirming:
1. The single-argument form is now DEPRECATED, not "the only valid form" as the reviewer asserted.
2. The recommended profile value IS literally the string `"max"` — it is not "fictional" in the installed version.
3. The TypeScript signature requires both arguments — replacing with the single-arg form fails type-check (`error TS2554: Expected 2 arguments, but got 1.`).

The existing `revalidateTag("plans", "max")` call matches Next.js's own deprecation-message-recommended migration path. The inline comment in the file already explains this in its current form. No code change is appropriate.

**Original issue summary from REVIEW.md:** Reviewer claimed the second `"max"` argument is undocumented and silently ignored, citing the single-argument form as the only valid one — this is incorrect for Next.js 16.2.4.

---

_Fixed: 2026-05-25T00:00:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
