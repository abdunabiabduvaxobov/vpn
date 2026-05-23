---
phase: 04-landing-surfaces
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - landing/next.config.ts
  - landing/src/i18n/routing.ts
  - landing/src/i18n/request.ts
  - landing/src/proxy.ts
  - landing/src/app/[locale]/layout.tsx
  - landing/src/messages/en.json
  - landing/src/messages/ru.json
  - landing/src/messages/es.json
  - landing/src/messages/uz.json
  - landing/src/lib/env.ts
  - landing/.env.example
  - landing/.env.local.example
autonomous: true
requirements:
  - WEB-08
must_haves:
  truths:
    - "Landing builds as a Node-runtime app (output: 'standalone'), not a static export"
    - "Locales available are exactly ru, en, es (uz removed)"
    - "All three message files contain every Phase 4 namespace key (login, dashboard, pricing, pay, errors, auth, navbar)"
    - "next-intl middleware continues to handle '/' → '/<defaultLocale>/' routing on the Node runtime"
  artifacts:
    - path: "landing/next.config.ts"
      provides: "Node-runtime Next.js config"
      contains: "output: \"standalone\""
    - path: "landing/src/i18n/routing.ts"
      provides: "locale list updated to ru/en/es"
      contains: "[\"ru\", \"en\", \"es\"]"
    - path: "landing/src/messages/es.json"
      provides: "Spanish message file with all Phase 4 keys"
    - path: "landing/src/lib/env.ts"
      provides: "typed env loader (server-only) — BACKEND_API_URL, REVALIDATE_SECRET, COOKIE_DOMAIN, NODE_ENV"
      exports: ["env"]
  key_links:
    - from: "landing/src/i18n/routing.ts"
      to: "next-intl middleware (landing/src/proxy.ts)"
      via: "createMiddleware(routing)"
      pattern: "createMiddleware\\(routing\\)"
    - from: "landing/src/lib/env.ts"
      to: "all server-only modules (Node proxy, OAuth callback, revalidate route)"
      via: "import { env } from '@/lib/env'"
      pattern: "from\\s+[\"']@/lib/env[\"']"
tags: [foundation, i18n, next-intl, env-config]
---

<objective>
Switch the landing from static export (`output: 'export'`) to Node runtime (`output: 'standalone'`) so we can run server-side OAuth callbacks, cookie-setting proxies, and on-demand ISR (D-01). Update locale list from `ru/en/uz` to `ru/en/es` (D-03). Pre-populate all three message files with every Phase 4 i18n namespace so downstream UI plans can reference keys without modifying messages again. Introduce a typed env loader (`landing/src/lib/env.ts`) so every Phase 4 server module reads `BACKEND_API_URL`, `REVALIDATE_SECRET`, `COOKIE_DOMAIN`, and `NODE_ENV` from one place with fail-fast validation.

Purpose: foundation for every other Phase 4 plan — they all assume a Node runtime, ES locale presence, and a single env source. Doing this once up-front prevents merge conflicts in 7 downstream plans.

Output: a landing that still builds (`npm run build`), still renders `/ru/`, `/en/`, `/es/` marketing pages, no longer renders `/uz/`, and exposes `landing/.next/standalone/` after build.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-UI-SPEC.md
@landing/next.config.ts
@landing/src/i18n/routing.ts
@landing/src/i18n/request.ts
@landing/src/proxy.ts
@landing/src/messages/en.json
@landing/src/app/[locale]/layout.tsx

<interfaces>
<!-- Locked CONTEXT.md decisions this plan implements -->
- D-01: output 'export' → 'standalone'. `unoptimized: true` on images can be dropped (Node runtime supports the default loader) — leave as `unoptimized: true` only if a marketing page relies on it; safer default is to remove.
- D-03: locales = ['ru','en','es'], defaultLocale = 'ru', localePrefix = 'always' (unchanged).
- Existing message files use top-level namespaces (`metadata`, `nav`, `hero`, `features`, `howItWorks`, `faq`, `footer`, `privacy`). Phase 4 ADDS: `login`, `dashboard`, `pricing`, `pay`, `auth`, `errors`, `navbar.app` (logged-in extension).

i18n key contract (every message file MUST contain these keys verbatim — copy from UI-SPEC §Copywriting Contract):

```json
{
  "login": {
    "heading": "...",
    "subhead": "...",
    "signIn": { "apple": "...", "google": "..." },
    "backHome": "..."
  },
  "dashboard": {
    "heading": "...",
    "email": "...",
    "plan": { "label": "...", "free": "...", "pro": "..." },
    "cta": { "getPro": "...", "manage": "..." },
    "signOut": "..."
  },
  "pricing": {
    "heading": "...",
    "subhead": "...",
    "currency": { "usd": "USD", "eur": "EUR", "rub": "RUB" },
    "period": { "monthly": "...", "yearly": "..." },
    "cta": { "getPro": "...", "current": "..." },
    "empty": { "heading": "..." }
  },
  "pay": {
    "success": {
      "processing": "...",
      "active": "...",
      "continue": "...",
      "takingLonger": { "heading": "...", "body": "..." },
      "refresh": "...",
      "contactSupport": "..."
    },
    "fail": {
      "title": "...",
      "body": { "default": "...", "declined": "...", "cancelled": "..." },
      "tryAgain": "...",
      "contactSupport": "..."
    }
  },
  "auth": {
    "signedOut": "...",
    "signOut": { "confirm": { "title": "...", "body": "...", "confirm": "...", "cancel": "..." } }
  },
  "errors": {
    "sessionExpired": "...",
    "network": "...",
    "oauthState": "...",
    "oauthDenied": "..."
  },
  "navbar": {
    "app": { "pricing": "...", "dashboard": "...", "login": "...", "signOut": "..." }
  }
}
```

ES initial translation: Claude does an initial pass (human translator review is a Phase 4 follow-up todo per CONTEXT.md deferred ideas). EN copy in UI-SPEC §Copywriting Contract is canonical.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: Flip next.config to standalone + drop uz locale + add es locale + drop uz from layout alternate languages</name>
  <files>landing/next.config.ts, landing/src/i18n/routing.ts, landing/src/app/[locale]/layout.tsx</files>
  <read_first>
    - landing/next.config.ts
    - landing/src/i18n/routing.ts
    - landing/src/app/[locale]/layout.tsx
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-01, D-03)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (theme strategy)
  </read_first>
  <action>
    Edit landing/next.config.ts: change `output: "export"` to `output: "standalone"`. Remove `trailingSlash: true` ONLY if it conflicts with any existing route — otherwise leave it. Drop `images.unoptimized` (Node runtime supports the default loader; if any existing page depends on it, leave it but add a TODO comment).

    Edit landing/src/i18n/routing.ts: change `locales: ["ru", "en", "uz"]` to `locales: ["ru", "en", "es"]`. Keep `defaultLocale: "ru"` and `localePrefix: "always"` unchanged.

    Edit landing/src/app/[locale]/layout.tsx generateMetadata: in `alternates.languages` block, REPLACE the `uz: "/uz/"` line with `es: "/es/"`. In the `openGraph.locale` ternary, replace `"uz" ? "uz_UZ"` with `"es" ? "es_ES"`.
  </action>
  <acceptance_criteria>
    - `grep -n 'output:\s*"standalone"' landing/next.config.ts` returns exactly 1 match
    - `grep -n 'output:\s*"export"' landing/next.config.ts` returns 0 matches
    - `grep -n '"ru".*"en".*"es"' landing/src/i18n/routing.ts` returns 1 match
    - `grep -n '"uz"' landing/src/i18n/routing.ts` returns 0 matches
    - `grep -n '"uz":' landing/src/app/[locale]/layout.tsx` returns 0 matches
    - `grep -n '"es":\s*"/es/"' landing/src/app/[locale]/layout.tsx` returns 1 match
  </acceptance_criteria>
  <done>next.config.ts is on standalone, routing.ts lists ru/en/es, and layout.tsx alternate languages match the new locale set.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: Rotate message files — delete uz.json, create es.json, add Phase 4 i18n namespaces to all three locale files</name>
  <files>landing/src/messages/en.json, landing/src/messages/ru.json, landing/src/messages/es.json, landing/src/messages/uz.json</files>
  <read_first>
    - landing/src/messages/en.json
    - landing/src/messages/ru.json
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Copywriting Contract — exact EN strings)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-22 — pay.success message keys)
  </read_first>
  <action>
    Delete landing/src/messages/uz.json (entire file).

    Create landing/src/messages/es.json: copy the COMPLETE structure of landing/src/messages/en.json (so all existing marketing keys exist) and translate every leaf string to Spanish. Initial machine-quality translation is acceptable; a follow-up native-speaker review is captured in CONTEXT.md deferred ideas.

    For each of en.json, ru.json, es.json — append the Phase 4 namespaces (login, dashboard, pricing, pay, auth, errors, navbar.app) at the same depth as existing top-level keys. Copy EN strings verbatim from UI-SPEC §Copywriting Contract (e.g., `login.signIn.apple` = "Sign in with Apple", `dashboard.signOut` = "Sign out", `pricing.heading` = "Choose your plan", `pay.success.processing` = "Activating your Pro subscription…", `pay.fail.title` = "Payment didn't go through", `errors.oauthState` = "Sign-in link expired. Please try again from the start."). For RU + ES, translate each value while preserving the JSON shape exactly. Use Spanish "iniciar sesión", Russian "Войти" etc. — keep punctuation including the ellipsis "…".

    Required key set (verify each appears in all three files):
    - login.heading, login.subhead, login.signIn.apple, login.signIn.google, login.backHome
    - dashboard.heading, dashboard.email, dashboard.plan.label, dashboard.plan.free, dashboard.plan.pro, dashboard.cta.getPro, dashboard.cta.manage, dashboard.signOut
    - pricing.heading, pricing.subhead, pricing.currency.usd, pricing.currency.eur, pricing.currency.rub, pricing.period.monthly, pricing.period.yearly, pricing.cta.getPro, pricing.cta.current, pricing.empty.heading
    - pay.success.processing, pay.success.active, pay.success.continue, pay.success.takingLonger.heading, pay.success.takingLonger.body, pay.success.refresh, pay.success.contactSupport
    - pay.fail.title, pay.fail.body.default, pay.fail.body.declined, pay.fail.body.cancelled, pay.fail.tryAgain, pay.fail.contactSupport
    - auth.signedOut, auth.signOut.confirm.title, auth.signOut.confirm.body, auth.signOut.confirm.confirm, auth.signOut.confirm.cancel
    - errors.sessionExpired, errors.network, errors.oauthState, errors.oauthDenied
    - navbar.app.pricing, navbar.app.dashboard, navbar.app.login, navbar.app.signOut

    Run `node -e "['en','ru','es'].forEach(l=>JSON.parse(require('fs').readFileSync('landing/src/messages/'+l+'.json','utf8')))"` to confirm all three are valid JSON before committing.
  </action>
  <acceptance_criteria>
    - `test ! -e landing/src/messages/uz.json` (file does not exist)
    - `test -e landing/src/messages/es.json` (file exists)
    - For L in en ru es: `node -e "const j=require('./landing/src/messages/'+'L'+'.json'); ['login.signIn.apple','dashboard.signOut','pricing.heading','pay.success.processing','pay.fail.title','errors.oauthState','auth.signedOut','navbar.app.pricing'].forEach(k=>{let x=j;k.split('.').forEach(p=>x=x?.[p]); if(!x) throw new Error('missing '+k+' in '+'L')})"` exits 0
    - `grep -c '"signIn"' landing/src/messages/en.json landing/src/messages/ru.json landing/src/messages/es.json` returns 1 for each file
    - `node -e "JSON.parse(require('fs').readFileSync('landing/src/messages/es.json','utf8'))"` exits 0
  </acceptance_criteria>
  <done>uz.json is gone, es.json exists with valid JSON, and all three locale files contain every Phase 4 i18n key listed above.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: Add typed server env loader (landing/src/lib/env.ts) + .env.example files + verify build</name>
  <files>landing/src/lib/env.ts, landing/.env.example, landing/.env.local.example</files>
  <read_first>
    - landing/src/lib/constants.ts (existing client-safe constants pattern)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-07 same-origin proxy, D-08 cookie names, D-14 revalidate secret)
    - landing/package.json (Node version / engines if present)
  </read_first>
  <action>
    Create landing/src/lib/env.ts as a server-only module (add `import "server-only"` at top — `server-only` is a Next.js built-in package). Export a frozen `env` object with the following typed fields, each validated at module load:

    ```ts
    import "server-only";

    type RequiredKey = "BACKEND_API_URL" | "REVALIDATE_SECRET";
    type OptionalKey = "COOKIE_DOMAIN" | "NODE_ENV";

    function readRequired(key: RequiredKey): string {
      const v = process.env[key];
      if (!v || v.trim() === "") {
        throw new Error(`[landing/env] Required env var ${key} is missing or empty`);
      }
      return v;
    }

    export const env = Object.freeze({
      BACKEND_API_URL: readRequired("BACKEND_API_URL"),       // e.g. https://vpnapi.mydayai.uz
      REVALIDATE_SECRET: readRequired("REVALIDATE_SECRET"),   // shared with backend admin handlers (D-14)
      COOKIE_DOMAIN: process.env.COOKIE_DOMAIN ?? "",          // empty = host-only cookie (default for dev)
      NODE_ENV: (process.env.NODE_ENV ?? "development") as "development" | "production" | "test",
      IS_PROD: process.env.NODE_ENV === "production",
    });
    ```

    Create landing/.env.example with documented placeholders (committed):
    ```
    # Backend API base URL (no trailing slash). Production: https://vpnapi.mydayai.uz
    BACKEND_API_URL=https://vpnapi.mydayai.uz

    # Shared secret with the Go backend's admin write handlers for /api/revalidate-pricing (D-14).
    # Backend will POST landing/api/revalidate-pricing?secret=<this>; landing constant-time-compares.
    REVALIDATE_SECRET=replace-me-32-bytes-base64

    # Cookie scope. Leave empty in dev to get host-only cookies. In production set to "risevpn.com".
    COOKIE_DOMAIN=

    # Standard Next.js
    NODE_ENV=development
    ```

    Create landing/.env.local.example as a copy of .env.example with placeholder local-dev values (BACKEND_API_URL=http://localhost:8080). Do NOT commit .env.local itself.

    Run `cd landing && npm run build` from the working directory to verify the standalone build works with the locale change (test env vars can be exported inline: `BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build`).
  </action>
  <acceptance_criteria>
    - `grep -n 'import "server-only"' landing/src/lib/env.ts` returns 1 match
    - `grep -n 'BACKEND_API_URL' landing/src/lib/env.ts` returns at least 2 matches
    - `grep -n 'REVALIDATE_SECRET' landing/src/lib/env.ts` returns at least 2 matches
    - `grep -n 'Object.freeze' landing/src/lib/env.ts` returns 1 match
    - `test -e landing/.env.example && test -e landing/.env.local.example`
    - `grep -n 'BACKEND_API_URL=' landing/.env.example` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0
    - `test -d landing/.next/standalone` (standalone build artifact emitted)
  </acceptance_criteria>
  <done>env.ts loads and freezes the four required vars, .env.example documents them, and `npm run build` succeeds producing landing/.next/standalone/.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| build-time → runtime | Env vars baked into the Node runtime; secrets must never leak to client bundles |
| process env → server-only modules | `env.ts` must be `server-only`; importing into a client component should hard-fail the build |
| URL path → locale resolver | `[locale]` dynamic segment fed by next-intl middleware; only allow-listed locales should reach React |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-01-01 | I (Info disclosure) | landing/src/lib/env.ts | mitigate | `import "server-only"` causes Webpack to throw at build time if a client component imports it (Task 3); `REVALIDATE_SECRET` never appears in `NEXT_PUBLIC_` namespace |
| T-04-01-02 | T (Tampering) | landing/src/i18n/routing.ts → `[locale]` segment | mitigate | next-intl's `defineRouting` enforces an allow-list; requests for `/zz/` already 404 via `hasLocale` check in `request.ts` and `layout.tsx` (Task 1 keeps the existing check) |
| T-04-01-03 | D (DoS) | next.config.ts standalone bundle | accept | Standalone bundle adds a Node attack surface vs static export; mitigated downstream by Plan 03 (proxy rate-limit handled at nginx) and Plan 02 (no client-side fetch on cold pages). Bare risk acceptable for the auth flows we now need. |
| T-04-01-04 | I (Info disclosure) | landing/.env.example | accept | Committed placeholders only; real secrets live in `.env.local` (uncommitted per existing .gitignore — verify before commit). |
| T-04-01-05 | T (Tampering) | landing/src/messages/*.json | mitigate | Translation strings are loaded server-side via static `import` (existing `request.ts` pattern); any future move to runtime fetch would require revisiting XSS escaping in React (React auto-escapes by default, so risk is bounded) |
</threat_model>

<verification>
- Build: `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0
- Standalone artifact: `test -d landing/.next/standalone` succeeds
- Locale routes: confirm `landing/.next/standalone` server can serve `/ru/`, `/en/`, `/es/` (manual or smoke test in Plan 08)
- No uz: `grep -rn '"uz"\|/uz/' landing/src/ landing/next.config.ts` returns 0 hits
- i18n key coverage: the JS one-liner in Task 2 acceptance criteria asserts all required keys exist
</verification>

<success_criteria>
- All Phase 4 i18n keys exist in en/ru/es message files (assertion script in Task 2)
- `output: "standalone"` produces a runnable Node bundle
- `env.ts` is server-only and validates required vars at load time
- The landing's existing marketing pages still render for `/ru/`, `/en/`, `/es/` (verified by build; full E2E in Plan 08)
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-01-foundation-i18n-standalone-SUMMARY.md` documenting:
- Locale migration (uz removed, es added)
- next.config switch (export → standalone) and any image-loader implication
- env.ts contract (downstream plans import from here)
- Initial ES translation quality flag (follow-up: native-speaker review)
</output>
