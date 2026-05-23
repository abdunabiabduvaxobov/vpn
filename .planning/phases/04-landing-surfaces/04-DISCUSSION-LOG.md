# Phase 4: Landing Surfaces - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-24
**Phase:** 04-landing-surfaces
**Areas discussed:** Deployment model, Locales, UI library, Hybrid split, OAuth flow, Pay-success UX, API+cookies, Pricing data flow, Dashboard scope, Pricing CTA, Currency, Theme+Sign-out

---

## Wave 1: Foundation (3 questions)

### Deployment Model

| Option | Description | Selected |
|--------|-------------|----------|
| Drop `output: export` → Node + ISR | Switch to `output: 'standalone'`, full Node runtime, ISR/middleware/HttpOnly all work | |
| Keep static export | nginx-serving-HTML; drop HttpOnly/ISR requirements | |
| Hybrid: static for marketing, Node for app pages | Static for /, faq, etc.; Node for /login, /dashboard, /pricing, /pay/* | ✓ |

**User's choice:** Hybrid

### Locales Scope

| Option | Description | Selected |
|--------|-------------|----------|
| Keep RU + EN + UZ (matches code) | Reality wins; update PROJECT.md to match | |
| Switch to RU + EN + ES (matches project docs) | Drop UZ, add ES; translate UZ messages to ES | ✓ |
| All four: RU + EN + UZ + ES | Keep what exists, add ES | |

**User's choice:** RU + EN + ES (drop UZ, add ES)

### UI Library

| Option | Description | Selected |
|--------|-------------|----------|
| Continue with @base-ui/react | Matches landing today; add Form/Card primitives | ✓ |
| Migrate landing to shadcn/ui | Refactor existing primitives; align with admin-web | |
| shadcn only for new pages | Two libraries coexist in landing | |

**User's choice:** Continue with @base-ui/react

---

## Wave 2: Implementation (3 questions)

### Hybrid Deployment — Execution Mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Single app, drop `output: export` | Switch to standalone; marketing pages stay SSG; one app, one deploy | ✓ |
| Two Next apps (marketing/ + app/) | Separate builds, separate deploys; max isolation | |
| Subdomain split (risevpn.com + app.risevpn.com) | New subdomain for app pages | |

**User's choice:** Single app, standalone runtime

### OAuth Callback

| Option | Description | Selected |
|--------|-------------|----------|
| Landing handles callback at /auth/callback?provider=apple\|google | Frontend extracts ID token, POSTs to backend, backend sets HttpOnly | ✓ |
| Backend handles callback (redirect_uri = vpnapi.mydayai.uz) | Cross-domain cookie required | |
| Popup window flow with postMessage | Avoid full-page redirect; popup-blocker risk | |

**User's choice:** Landing handles callback

### /pay/success Polling UX

| Option | Description | Selected |
|--------|-------------|----------|
| Spinner → success in <2s, 'check email' after 30s | Matches SC #4 verbatim; 5 polls before escalate; 30s timeout | ✓ |
| Optimistic success → validate in background | Show Pro active immediately; risk of regression | |
| Manual 'Click to verify' button | No auto-polling; cheapest, weakest UX | |

**User's choice:** Spinner with 30s window

---

## Wave 3: Architecture (3 questions)

### API + Cookies Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Same-origin proxy at landing /api/* | Cookies on risevpn.com; no cross-domain; +5–20ms server-hop | ✓ |
| Cross-domain cookies on .risevpn.com parent | Move backend to api.risevpn.com; DNS migration required | |
| Token in JS memory + Authorization header | No HttpOnly; borderline SC #1 violation | |

**User's choice:** Same-origin proxy

### /pricing Data Flow

| Option | Description | Selected |
|--------|-------------|----------|
| On-demand ISR with revalidateTag | Build-time render; admin write triggers revalidate; matches SC #5 | ✓ |
| SSR every request | Always fresh; backend round-trip per page load | |
| Client-side fetch with skeleton | Static-export compatible; worse UX | |

**User's choice:** On-demand ISR

### /dashboard Scope

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal: email + plan + single CTA + Sign out | Matches SC #1 verbatim; defer device list/billing to Phase 7 | ✓ |
| Extended: + device list + cancel + billing history | More surface; Phase 7 work pulled forward | |
| Strictest minimal: just Sign out, no Manage Subscription | Email + plan + Get Pro + Sign out only | |

**User's choice:** Minimal with Manage Subscription link for Pro

---

## Wave 4: UX Details (3 questions)

### /pricing CTA (Logged-Out)

| Option | Description | Selected |
|--------|-------------|----------|
| /login?next=...&plan=...&period=... → auto-checkout on return | Zero extra clicks after sign-in; matches SC #3 | ✓ |
| Return to /pricing with selection prefilled → user confirms | Adds friction; safer review step | |
| Modal sign-in on /pricing (no redirect) | Popup-blocker + provider iframe restrictions | |

**User's choice:** Auto-checkout on return

### Currency per Locale

| Option | Description | Selected |
|--------|-------------|----------|
| RU→RUB, EN→USD, ES→EUR | Market-natural; extends Phase 3 D-27 | ✓ |
| RU→RUB, all others→USD | Honors Phase 3 D-27 verbatim; ES users see USD | |
| User picks on first visit | One-time chooser; no auto-derivation | |

**User's choice:** Market-natural mapping

### Theme + Sign-Out

| Option | Description | Selected |
|--------|-------------|----------|
| Theme: system; Sign-out: POST /auth/logout + clear cookies + redirect / | Standard pattern; revoke refresh server-side | ✓ |
| Theme: light-lock for commerce pages; Sign-out: same | Conversion claim for light commerce pages | |
| Theme: system; Sign-out: client-only (no backend call) | Faster, weaker security posture | |

**User's choice:** System theme + full backend logout

---

## Claude's Discretion

- /pay/fail content structure (within D-23 constraints)
- Loading skeletons across all app pages
- Apple/Google button styling (must match each provider's brand guide)
- Mobile menu sheet content
- Error toasts vs inline error messages on /login
- 404 / 500 patterns
- ES translation initial pass (with human review as a follow-up todo)
- Server-side logging shape

## Deferred Ideas

- Device list / revoke (Phase 7)
- Billing history (Phase 7)
- In-app subscription cancel UI (Phase 7; backend ready from 03-05)
- ES translation human review (Phase 4 follow-up todo, not phase blocker)
- PROJECT.md updates: confirm base-ui/landing + RU/EN/ES locale list
- Phase 3 follow-up: add revalidateTag fan-out to 10 admin write handlers
- Apple/Google dashboard config (operator task)
- Production DNS cutover vpn.mydayai.uz → risevpn.com (operator task)
