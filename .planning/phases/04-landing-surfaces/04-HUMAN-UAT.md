---
status: partial
phase: 04-landing-surfaces
source: [04-VERIFICATION.md]
started: 2026-05-26T10:05:00Z
updated: 2026-05-26T10:05:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Apple Sign-In End-to-End (SC#1 / WEB-01)
expected: Visit /ru/login, click "Sign in with Apple", complete Apple ID auth with live APPLE_SERVICE_ID + .p8 key. Land on /ru/dashboard showing email, plan="free", "Get Pro" link. DevTools localStorage empty; rv_at, rv_rt, rv_user cookies all HttpOnly=true.
result: [pending]

### 2. Google Sign-In End-to-End (SC#1 / WEB-01 — Google variant)
expected: Visit /ru/login, click "Sign in with Google", complete Google OAuth with live GOOGLE_CLIENT_ID_WEB. Land on /dashboard with session, no JWT in localStorage, HttpOnly cookies set.
result: [pending]

### 3. UserMenu Sign-Out Popover (SC#6 / WEB-09)
expected: Log in (real session), visit /ru/pricing, click avatar trigger in navbar. Popover renders with email and "Выйти"/"Sign out" button. Clicking POSTs to /api/auth/logout (200), clears session cookies, redirects to /ru/login.
result: [pending]

### 4. Pro Activation Full Flow (lava.top sandbox)
expected: Log in, visit /ru/pricing, click "Get Pro" (monthly), complete payment on lava.top sandbox. Redirected to /ru/pay/success; "Активируем…" → success ("Pro is active!") within ~2s. /ru/dashboard shows plan="Pro". Network: POST /api/v1/auth/refresh fired before success UI.
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
