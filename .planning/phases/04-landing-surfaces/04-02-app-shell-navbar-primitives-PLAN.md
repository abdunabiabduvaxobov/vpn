---
phase: 04-landing-surfaces
plan: 02
type: execute
wave: 2
depends_on: [04-01]
files_modified:
  - landing/src/components/ui/card.tsx
  - landing/src/components/ui/skeleton.tsx
  - landing/src/components/ui/toast.tsx
  - landing/src/components/app/tier-badge.tsx
  - landing/src/components/app/user-menu.tsx
  - landing/src/components/common/navbar-app.tsx
  - landing/src/lib/utils.ts
  - landing/src/app/[locale]/(app)/layout.tsx
  - landing/src/lib/session.ts
  - landing/public/brand/apple/apple-sign-in.svg
  - landing/public/brand/google/google-g.svg
autonomous: true
requirements:
  - WEB-09
must_haves:
  truths:
    - "A logged-out visitor sees a navbar with 'Pricing' + 'Login' links (server-rendered, no flash of authenticated content)"
    - "A logged-in visitor (cookie `rv_at` present) sees a navbar with 'Pricing' + 'Dashboard' + 'Sign out' (via UserMenu)"
    - "The new app-page layout group `(app)` exists and applies `dynamic = 'force-dynamic'` so cookie reads happen per request"
    - "Reusable Card/Skeleton/Toast/TierBadge primitives live in the existing base-ui style"
  artifacts:
    - path: "landing/src/components/ui/card.tsx"
      provides: "Card primitive — surface bg, rounded-xl, border-subtle"
      exports: ["Card", "CardHeader", "CardTitle", "CardContent", "CardFooter"]
    - path: "landing/src/components/ui/skeleton.tsx"
      provides: "Loading skeleton — bg-surface-elevated/40 animate-pulse"
      exports: ["Skeleton"]
    - path: "landing/src/components/ui/toast.tsx"
      provides: "Toast wrapper — base-ui Toast.Provider + helper"
      exports: ["Toaster", "toast"]
    - path: "landing/src/components/app/tier-badge.tsx"
      provides: "Free / Pro badge pill"
      exports: ["TierBadge"]
    - path: "landing/src/components/app/user-menu.tsx"
      provides: "Logged-in navbar dropdown with email + Sign out"
      exports: ["UserMenu"]
    - path: "landing/src/components/common/navbar-app.tsx"
      provides: "App-page navbar with server-side logged-in/out branching"
      exports: ["NavbarApp"]
    - path: "landing/src/app/[locale]/(app)/layout.tsx"
      provides: "Route-group layout for /login /dashboard /pricing /pay/* — force-dynamic + NavbarApp"
    - path: "landing/src/lib/session.ts"
      provides: "Server-only helper that reads rv_at / rv_user cookies and returns {isAuthed, email, planId}"
      exports: ["getSession"]
  key_links:
    - from: "landing/src/components/common/navbar-app.tsx"
      to: "landing/src/lib/session.ts"
      via: "async getSession() server call"
      pattern: "getSession\\("
    - from: "landing/src/app/[locale]/(app)/layout.tsx"
      to: "landing/src/components/common/navbar-app.tsx"
      via: "<NavbarApp /> in JSX"
      pattern: "<NavbarApp"
tags: [foundation, components, navbar, app-shell]
---

<objective>
Build the app-page shell: a `(app)` route group with its own `layout.tsx` that uses `dynamic = 'force-dynamic'`, a new `NavbarApp` component that renders different links depending on whether the `rv_at` cookie is present (satisfies WEB-09 / SC #6), and the small set of reusable UI primitives (`Card`, `Skeleton`, `Toast`, `TierBadge`, `UserMenu`) that downstream plans will compose. Also vendor the official Apple and Google brand-mark SVGs so Plan 04 can render compliant sign-in buttons.

Purpose: foundation for `/login`, `/dashboard`, `/pricing`, `/pay/success`, `/pay/fail` — all five live under the `(app)` group and reuse these primitives. The navbar's server-side logged-in detection (D-18) is the single hardest correctness requirement for SC #6.

Output: every Phase 4 page in waves ≥3 can `import { Card } from "@/components/ui/card"`, drop into the `(app)` route group, and get the right navbar automatically. No client-side cookie sniffing anywhere.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-UI-SPEC.md
@.planning/phases/04-landing-surfaces/04-01-foundation-i18n-standalone-PLAN.md
@landing/src/components/ui/button.tsx
@landing/src/components/ui/sheet.tsx
@landing/src/components/common/navbar.tsx
@landing/src/components/common/locale-switcher.tsx
@landing/src/components/common/logo.tsx
@landing/src/app/[locale]/layout.tsx
@landing/src/i18n/navigation.ts
@landing/src/app/globals.css

<interfaces>
<!-- Locked CONTEXT.md decisions this plan implements -->
- D-05: continue with @base-ui/react. No shadcn install. Card / Skeleton / Toast are minimal wrappers built directly with Tailwind 4 tokens already defined in `globals.css`.
- D-06: reuse Navbar/LocaleSwitcher/Logo/Button/Sheet from existing landing primitives.
- D-08 (preview): cookie name is exactly `rv_at` (access token, HttpOnly). This plan ONLY READS the cookie for navbar branching — setting it is owned by Plan 03 (proxy) and Plan 04 (OAuth callback).
- D-17 (preview): a SECOND cookie `rv_user` (HttpOnly, signed JSON of `{email, planId}`) is set at OAuth callback time. Defined in Plan 04. THIS plan reads it through `getSession()` IF PRESENT and falls back gracefully when absent (logged-out users won't have it).
- D-18: server-side cookie detection happens in a Server Component via `cookies()` from `next/headers`.

Color/typography tokens used (already defined in `globals.css` — DO NOT introduce new ones):
- Card bg: `bg-surface` (or `bg-surface-elevated` for elevated variant)
- Card border: `border border-border-subtle`
- Card radius: `rounded-[var(--radius-xl)]` (i.e. `rounded-3xl` won't match — must use the CSS var)
- Skeleton: `bg-surface-elevated/40 animate-pulse rounded-[var(--radius-md)]`
- TierBadge Pro: `bg-accent text-accent-foreground`
- TierBadge Free: `border border-border text-muted-foreground`

`cookies()` API for Next.js 16 server components:
```ts
import { cookies } from "next/headers";
const jar = await cookies();
const at = jar.get("rv_at")?.value; // string | undefined
```

Apple brand-mark SVG: download from `https://developer.apple.com/design/human-interface-guidelines/sign-in-with-apple/` (button asset). Vendor exactly as supplied to `landing/public/brand/apple/apple-sign-in.svg`. License: Apple permits use specifically for "Sign in with Apple" buttons.

Google brand-mark SVG: download the official "G" logo from `https://developers.google.com/identity/branding-guidelines#logo`. Vendor to `landing/public/brand/google/google-g.svg`. License: Google permits use specifically for "Sign in with Google" buttons.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: Add UI primitives — Card, Skeleton, Toast, TierBadge — and ensure cn() utility exists</name>
  <files>landing/src/components/ui/card.tsx, landing/src/components/ui/skeleton.tsx, landing/src/components/ui/toast.tsx, landing/src/components/app/tier-badge.tsx, landing/src/lib/utils.ts</files>
  <read_first>
    - landing/src/components/ui/button.tsx (existing base-ui + cva pattern to mirror)
    - landing/src/components/ui/sheet.tsx (base-ui Dialog pattern)
    - landing/src/lib/utils.ts (if exists — check for existing cn export)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Color, §Spacing, §Registry Safety — primitives list)
    - landing/src/app/globals.css (token names)
  </read_first>
  <action>
    First check if `landing/src/lib/utils.ts` exists. If not, create it:
    ```ts
    import { clsx, type ClassValue } from "clsx";
    import { twMerge } from "tailwind-merge";
    export function cn(...inputs: ClassValue[]) {
      return twMerge(clsx(inputs));
    }
    ```

    Create landing/src/components/ui/card.tsx — base-ui-style primitives, no client directive (pure styled divs):
    - `Card` — `<div className="rounded-[var(--radius-xl)] border border-border-subtle bg-surface p-6 ..." />`
    - `CardHeader` — `<div className="flex flex-col gap-1.5 pb-4" />`
    - `CardTitle` — `<h2 className="text-2xl font-semibold font-heading" />`
    - `CardContent` — `<div className="flex flex-col gap-4" />`
    - `CardFooter` — `<div className="flex items-center justify-end gap-2 pt-4" />`
    Forward ref + spread className via cn().

    Create landing/src/components/ui/skeleton.tsx:
    ```tsx
    import { cn } from "@/lib/utils";
    export function Skeleton({ className, ...rest }: React.HTMLAttributes<HTMLDivElement>) {
      return <div className={cn("bg-surface-elevated/40 animate-pulse rounded-[var(--radius-md)]", className)} {...rest} />;
    }
    ```

    Create landing/src/components/ui/toast.tsx using @base-ui/react's Toast primitive:
    ```tsx
    "use client";
    import { Toast } from "@base-ui/react/toast";
    // export <Toaster /> that mounts Toast.Provider + Toast.Viewport (top-right)
    // export `toast({ title, description, variant: 'default' | 'destructive' })` helper that pushes via Toast.useToastManager()
    ```
    If @base-ui/react/toast is not exposed in the installed version (1.4.1), fall back to a minimal in-memory event-bus + a fixed-position list of `<div role="status">` toasts. Document the choice in the SUMMARY.

    Create landing/src/components/app/tier-badge.tsx:
    ```tsx
    type Props = { tier: "free" | "pro"; className?: string };
    export function TierBadge({ tier, className }: Props) {
      const t = useTranslations("dashboard.plan");
      return <span className={cn(
        "inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium",
        tier === "pro" ? "bg-accent text-accent-foreground" : "border border-border text-muted-foreground",
        className
      )}>{t(tier)}</span>;
    }
    ```
    NOTE: this component uses `useTranslations` so it MUST be a Client Component (`"use client"` at top) OR accept the label as a prop. For simplicity in server components, prefer the prop variant: `<TierBadge tier="pro" label={t('dashboard.plan.pro')} />`. Implement the prop variant.
  </action>
  <acceptance_criteria>
    - `test -e landing/src/lib/utils.ts && grep -n 'export function cn' landing/src/lib/utils.ts` returns 1 match
    - `grep -n 'bg-surface' landing/src/components/ui/card.tsx` returns at least 1 match
    - `grep -n 'rounded-\[var(--radius-xl)\]' landing/src/components/ui/card.tsx` returns 1 match
    - `grep -n 'animate-pulse' landing/src/components/ui/skeleton.tsx` returns 1 match
    - `grep -n '"use client"' landing/src/components/ui/toast.tsx` returns 1 match
    - `grep -n 'bg-accent' landing/src/components/app/tier-badge.tsx` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>Card, Skeleton, Toast, TierBadge primitives compile, follow existing token contract, and live alongside the existing button.tsx pattern.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: Add server-only session helper (lib/session.ts), NavbarApp with server-side logged-in branching, UserMenu, and (app) route-group layout</name>
  <files>landing/src/lib/session.ts, landing/src/components/common/navbar-app.tsx, landing/src/components/app/user-menu.tsx, landing/src/app/[locale]/(app)/layout.tsx</files>
  <read_first>
    - landing/src/components/common/navbar.tsx (existing marketing navbar — base-ui Sheet pattern)
    - landing/src/components/common/logo.tsx
    - landing/src/components/common/locale-switcher.tsx
    - landing/src/i18n/navigation.ts
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-08 cookie names, D-15 minimal dashboard, D-18 server-side detection, D-25 sign-out flow)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Page-Specific Layout Contracts — navbar variant)
  </read_first>
  <action>
    Create landing/src/lib/session.ts (server-only):
    ```ts
    import "server-only";
    import { cookies } from "next/headers";

    export type Session =
      | { isAuthed: false }
      | { isAuthed: true; email: string; planId: string };

    export async function getSession(): Promise<Session> {
      const jar = await cookies();
      const at = jar.get("rv_at")?.value;
      if (!at) return { isAuthed: false };
      const userRaw = jar.get("rv_user")?.value;
      if (!userRaw) {
        // Cookie de-sync — treat as authed but unknown identity.
        // Downstream pages can either fetch /api/me via the proxy or fall back to "you" / "—".
        return { isAuthed: true, email: "", planId: "" };
      }
      try {
        // rv_user is base64url(JSON({email, planId})); HMAC verification is owned by Plan 04 (setSessionCookie).
        // For Phase 4 we trust the HttpOnly+Secure+SameSite=Strict scope to prevent injection. A future
        // hardening pass should switch to JWT-signed cookie. (Note: see threat T-04-02-02.)
        const json = JSON.parse(Buffer.from(userRaw, "base64url").toString("utf8"));
        const email = typeof json.email === "string" ? json.email : "";
        const planId = typeof json.planId === "string" ? json.planId : "";
        return { isAuthed: true, email, planId };
      } catch {
        return { isAuthed: true, email: "", planId: "" };
      }
    }
    ```

    Create landing/src/components/app/user-menu.tsx — base-ui Popover with email + Sign out item. Renders trigger as Avatar/circle with first letter of email (or a user icon if email is empty). Sign out button is a `<form action="/api/auth/logout" method="POST">` so it works without JS. The form submission is intercepted by the Plan 03 proxy. Use base-ui Popover. `"use client"` directive.

    Create landing/src/components/common/navbar-app.tsx as a SERVER COMPONENT (no "use client"):
    ```tsx
    import { Link } from "@/i18n/navigation";
    import { getTranslations } from "next-intl/server";
    import { getSession } from "@/lib/session";
    import { Logo } from "./logo";
    import { LocaleSwitcher } from "./locale-switcher";
    import { buttonVariants } from "@/components/ui/button";
    import { UserMenu } from "@/components/app/user-menu";

    export async function NavbarApp() {
      const t = await getTranslations("navbar.app");
      const session = await getSession();
      return (
        <header className="sticky top-0 z-50 w-full border-b border-border-subtle bg-background/70 backdrop-blur-xl">
          <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 md:px-6 lg:px-8">
            <Link href="/" aria-label="Rise VPN — home"><Logo /></Link>
            <nav className="hidden items-center gap-8 md:flex">
              <Link href="/pricing" className="text-sm text-muted-foreground hover:text-foreground">{t("pricing")}</Link>
              {session.isAuthed && (
                <Link href="/dashboard" className="text-sm text-muted-foreground hover:text-foreground">{t("dashboard")}</Link>
              )}
            </nav>
            <div className="flex items-center gap-2 md:gap-3">
              <LocaleSwitcher className="hidden sm:inline-flex" />
              {session.isAuthed ? (
                <UserMenu email={session.email} />
              ) : (
                <Link href="/login" className={buttonVariants({ size: "sm" })}>{t("login")}</Link>
              )}
            </div>
          </div>
        </header>
      );
    }
    ```

    Create landing/src/app/[locale]/(app)/layout.tsx — route-group layout. The `(app)` segment is a route group (no URL impact). This layout REPLACES the parent `[locale]/layout.tsx`'s `Navbar` with `NavbarApp` and adds `dynamic = 'force-dynamic'`. CRITICAL: route-group layouts do NOT skip the parent layout — the parent already wraps `<html><body><NextIntlClientProvider>...<Navbar/><main>{children}</main></NextIntlClientProvider>`. We need to (a) avoid the marketing `<Navbar/>` rendering on app pages and (b) inject our `<NavbarApp/>`.

    Approach: edit landing/src/app/[locale]/layout.tsx so that `<Navbar/>` is NOT rendered at the [locale] level. Move `<Navbar/>` into a new route group `(marketing)/layout.tsx` that wraps the existing marketing pages. The (app) group's layout renders `<NavbarApp/>` instead. Net file moves:
      - landing/src/app/[locale]/layout.tsx — REMOVE `<Navbar />` and `<Footer />`; keep html/body/NextIntlClientProvider/JSON-LD scripts. Children are rendered bare.
      - landing/src/app/[locale]/(marketing)/layout.tsx — NEW (or move existing page.tsx + privacy into the group). Wraps children with `<Navbar/>` + `<Footer/>`. The existing `page.tsx` and `privacy/` directory move under `(marketing)/`.
      - landing/src/app/[locale]/(app)/layout.tsx — NEW. Adds `export const dynamic = 'force-dynamic'`; renders `<NavbarApp />{children}<Footer />` (or just `<NavbarApp />{children}` if footer is undesired on app pages — per UI-SPEC `/auth/callback` has no chrome, but `/dashboard` / `/pricing` benefit from Footer; default: include Footer).

    Path moves to perform:
      - mv landing/src/app/[locale]/page.tsx → landing/src/app/[locale]/(marketing)/page.tsx
      - mv landing/src/app/[locale]/privacy → landing/src/app/[locale]/(marketing)/privacy
      - mv landing/src/app/[locale]/opengraph-image.tsx → landing/src/app/[locale]/(marketing)/opengraph-image.tsx (route-group files are scoped, this stays at this URL anyway because route groups don't affect URL)
      - mv landing/src/app/[locale]/not-found.tsx — KEEP at [locale]/ level (shared by both groups)
  </action>
  <acceptance_criteria>
    - `grep -n 'import "server-only"' landing/src/lib/session.ts` returns 1 match
    - `grep -n 'rv_at\|rv_user' landing/src/lib/session.ts` returns at least 2 matches
    - `grep -n 'getSession' landing/src/components/common/navbar-app.tsx` returns at least 1 match
    - `grep -n 'session.isAuthed' landing/src/components/common/navbar-app.tsx` returns at least 2 matches (one for Dashboard link, one for UserMenu vs Login)
    - `grep -n "force-dynamic" landing/src/app/\[locale\]/\(app\)/layout.tsx` returns 1 match
    - `grep -n '<Navbar' landing/src/app/\[locale\]/layout.tsx` returns 0 matches (Navbar moved to (marketing) group)
    - `test -e landing/src/app/\[locale\]/\(marketing\)/page.tsx`
    - `test -e landing/src/app/\[locale\]/\(marketing\)/privacy`
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0
  </acceptance_criteria>
  <done>NavbarApp branches server-side on rv_at cookie, the (app) route group has force-dynamic, the (marketing) group still renders the existing Navbar+Footer for `/`, and `npm run build` succeeds.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: Vendor Apple and Google brand-mark SVGs into landing/public/brand/</name>
  <files>landing/public/brand/apple/apple-sign-in.svg, landing/public/brand/google/google-g.svg, landing/public/brand/README.md</files>
  <read_first>
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Registry Safety — brand-mark requirement)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-11 — Apple Service ID context)
  </read_first>
  <action>
    Download official Apple "Sign in with Apple" button SVG from Apple HIG (https://developer.apple.com/design/human-interface-guidelines/sign-in-with-apple). The page provides a Buttons download (SwiftUI + web assets). Vendor the "Continue with Apple" black-style web SVG to `landing/public/brand/apple/apple-sign-in.svg`. If a direct SVG cannot be located, create a faithful 1:1 SVG replica using the published spec: white logo on black background, 44px minimum height, SF Pro Display font label, rounded corners radius 6px. Apple's text-mark or logomark only is acceptable IF the button label is rendered in adjacent React (`<button><AppleLogo/>{t('login.signIn.apple')}</button>`).

    Download official Google "G" logomark SVG from Google branding guidelines (https://developers.google.com/identity/branding-guidelines#logo — they provide a downloadable Google_G_Logo.svg). Vendor to `landing/public/brand/google/google-g.svg`. The wrapping button is composed in React in Plan 04 (background white, text "Sign in with Google" in Roboto, height ≥44px).

    Create landing/public/brand/README.md documenting:
    - Source URLs for both assets
    - License terms (Apple: only on Sign in with Apple buttons; Google: only on Sign in with Google buttons)
    - Replacement procedure if Apple/Google update their brand guidelines

    If outbound network access is restricted in the executor environment, leave a placeholder SVG of the correct dimensions + a TODO line in the README so the operator can drop in the real asset before any production push. Acceptance criteria still require the files to exist with valid SVG content.
  </action>
  <acceptance_criteria>
    - `test -s landing/public/brand/apple/apple-sign-in.svg` (non-empty)
    - `test -s landing/public/brand/google/google-g.svg` (non-empty)
    - `head -c 4 landing/public/brand/apple/apple-sign-in.svg | grep -q '<svg\|<?xm'`
    - `head -c 4 landing/public/brand/google/google-g.svg | grep -q '<svg\|<?xm'`
    - `test -e landing/public/brand/README.md`
    - `grep -ni 'apple\|google' landing/public/brand/README.md` returns at least 2 matches (mentions both vendors)
  </acceptance_criteria>
  <done>Both brand-mark SVGs exist on disk at the agreed paths, the README documents sources/license, and Plan 04 can `<Image src="/brand/apple/apple-sign-in.svg" />`.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser cookie → Server Component | `getSession()` reads cookies set by Plan 03/04 — must treat parsed contents as untrusted JSON |
| `(app)` route group → marketing pages | Layout separation ensures marketing pages stay static; app pages stay dynamic with cookie reads |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-02-01 | T (Tampering) | `getSession()` parses `rv_user` cookie as JSON | mitigate | HttpOnly + Secure + SameSite=Strict makes the cookie unreadable/unwritable to JS; the cookie's content is set by Plan 03 server-side after backend validates the OAuth ID token. Task 2 wraps `JSON.parse` in try/catch + type-checks each field; malformed → treat as anonymous |
| T-04-02-02 | T (Tampering) | `rv_user` cookie integrity | mitigate (deferred to Plan 03) | Phase 4 trusts HttpOnly + SameSite=Strict to prevent tampering. Plan 03 ships HMAC-signed cookie payload (`base64url(json).hmac`) so a future attacker with cookie-write access can't forge identity. NOTE in SUMMARY: HMAC verification lives in Plan 03's `setSessionCookie()`/`readSessionCookie()` helpers; this plan's `getSession()` will switch to call those helpers after Plan 03 lands |
| T-04-02-03 | S (Spoofing) | navbar logged-in detection by mere presence of `rv_at` | mitigate | `rv_at` is HttpOnly so client JS can't fabricate it; only the Plan 03 proxy sets it after backend `/auth/apple`/`/auth/google` returns success. Tampering reduces to "stole an HttpOnly cookie", which is the standard session-theft model |
| T-04-02-04 | I (Info disclosure) | UserMenu renders user email | accept | Email is non-sensitive after sign-in; existing apps universally show user identifier in nav. Risk: shoulder-surfing reveals account identifier — accepted |
| T-04-02-05 | I (Info disclosure) | session.ts module is `server-only` | mitigate | `import "server-only"` causes a build error if a client component imports it — prevents accidentally shipping cookie-parsing logic to the browser bundle |
| T-04-02-06 | T (Brand spoofing) | Apple/Google SVG assets | mitigate | Task 3 vendors official assets + README documents source URLs; ops can refresh from official site if Apple/Google change guidelines |
</threat_model>

<verification>
- `npm run build` succeeds with new (app) and (marketing) route groups
- `getSession()` returns `{isAuthed: false}` when no cookie is set (unit-testable via mock cookies — covered in Plan 08 smoke)
- A `curl http://localhost:3000/ru/` (no cookie) shows the marketing Navbar (Pricing-Login wording matches marketing namespace), and a future `curl http://localhost:3000/ru/pricing` (Plan 05) shows NavbarApp variant — full E2E in Plan 08
- TypeScript: `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npx tsc --noEmit` exits 0
</verification>

<success_criteria>
- (app) layout has `dynamic = 'force-dynamic'` so cookies are checked per request
- NavbarApp server-renders different links based on cookie presence (WEB-09 / SC #6)
- Card/Skeleton/Toast/TierBadge primitives exist and follow the design-token contract
- Brand-mark SVGs vendored at the agreed paths for Plan 04 to consume
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-02-app-shell-navbar-primitives-SUMMARY.md` documenting:
- Route-group migration (marketing pages moved under `(marketing)`, app pages stay under `(app)`)
- `getSession()` contract (cookies → Session discriminated union)
- Toast implementation choice (base-ui native vs custom fallback)
- Brand-mark asset state (live download vs placeholder)
- HMAC verification on `rv_user` punted to Plan 03 with explicit note
</output>
