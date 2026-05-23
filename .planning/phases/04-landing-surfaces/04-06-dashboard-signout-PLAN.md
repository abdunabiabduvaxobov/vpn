---
phase: 04-landing-surfaces
plan: 06
type: execute
wave: 3
depends_on: [04-02, 04-03]
files_modified:
  - landing/src/app/[locale]/(app)/dashboard/page.tsx
  - landing/src/app/[locale]/(app)/dashboard/signout-button.tsx
  - landing/src/components/app/dashboard-card.tsx
autonomous: true
requirements:
  - WEB-03
  - WEB-09
must_haves:
  truths:
    - "GET /<locale>/dashboard renders ONLY when rv_at cookie is present; otherwise server-side redirects to /<locale>/login?next=/dashboard"
    - "Dashboard shows the user's email (from rv_user cookie) and current plan name + tier badge (resolved from rv_user.planId via plan name lookup)"
    - "Because Plan 03 re-issues rv_user with the JWT's plan_id claim on every refresh rotation, and Plan 07 forces a refresh after a paid invoice, this page reflects the user's CURRENT plan (D-17 closure)"
    - "Free users see a single 'Get Pro' CTA → /<locale>/pricing"
    - "Pro users see a single 'Manage Subscription' link (deferred backend endpoint — Phase 4 renders link to Telegram support as a graceful fallback)"
    - "Sign out button opens a confirmation dialog; confirming POSTs to /api/auth/logout (Plan 03) and navigates to /<locale>/"
  artifacts:
    - path: "landing/src/app/[locale]/(app)/dashboard/page.tsx"
      provides: "Dashboard server page — gated by getSession()"
    - path: "landing/src/app/[locale]/(app)/dashboard/signout-button.tsx"
      provides: "SignOutButton with confirmation dialog"
      exports: ["SignOutButton"]
    - path: "landing/src/components/app/dashboard-card.tsx"
      provides: "DashboardCard — email row + plan row + CTA"
      exports: ["DashboardCard"]
  key_links:
    - from: "landing/src/app/[locale]/(app)/dashboard/page.tsx"
      to: "landing/src/lib/session.ts → getSession()"
      via: "redirect if !isAuthed"
      pattern: "getSession\\(|redirect\\(.*login"
    - from: "landing/src/app/[locale]/(app)/dashboard/signout-button.tsx"
      to: "POST /api/auth/logout (Plan 03)"
      via: "fetch('/api/auth/logout', { method: 'POST' })"
      pattern: "/api/auth/logout"
    - from: "landing/src/app/[locale]/(app)/dashboard/page.tsx"
      to: "landing/src/lib/plans.ts → fetchPlans"
      via: "look up plan name from planId"
      pattern: "fetchPlans"
tags: [dashboard, auth-gated, signout]
---

<objective>
Build the protected /dashboard page (WEB-03) that displays the signed-in user's email, current plan, and a single context-aware CTA. The page is server-side gated: missing `rv_at` → `redirect("/<locale>/login?next=/dashboard")`. Email and planId come from the `rv_user` cookie set at OAuth completion (Plan 04) and refreshed at every proxy refresh-rotation (Plan 03); the plan name is looked up by fetching `/api/v1/plans` server-side. Sign out is wired to Plan 03's `/api/auth/logout` route.

Purpose: WEB-03 closure + UX completeness — the dashboard is what makes "signed in" tangible to the user. Without it, sign-in feels like it did nothing. With Plan 03's B2 fix (rv_user re-issued with fresh plan_id on every refresh) and Plan 07's force-refresh trigger on paid status, this page reliably shows Pro immediately after the user returns from /pay/success.

Output: A logged-in user lands on `/<locale>/dashboard`, sees their email + plan + relevant CTA + sign-out, and signing out returns them to the marketing home page with all session cookies cleared.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-UI-SPEC.md
@.planning/phases/04-landing-surfaces/04-02-app-shell-navbar-primitives-PLAN.md
@.planning/phases/04-landing-surfaces/04-03-node-proxy-cookies-refresh-PLAN.md
@.planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-PLAN.md
@landing/src/lib/session.ts
@landing/src/lib/cookies.ts
@landing/src/lib/plans.ts
@landing/src/components/ui/card.tsx
@landing/src/components/app/tier-badge.tsx
@landing/src/i18n/navigation.ts

<interfaces>
<!-- Locked CONTEXT.md decisions -->
- D-15: minimal dashboard — email + plan + ONE CTA + Sign-out. No device list, no billing history. **W3 scope note: billing history (WEB-03 expanded scope) is intentionally OUT of Phase 4 — deferred to Phase 7+ per CONTEXT D-15 locked decision.**
- D-16: Manage Subscription URL needs a new backend endpoint `GET /api/v1/subscription/manage-url`. Endpoint DOES NOT EXIST in Phase 3 (verified). Phase 4 fallback: when user is Pro, link points to `https://t.me/flawlssr` (SUPPORT.telegram from constants) with copy "Manage Subscription" and a small caption "Contact support to manage your plan". A follow-up todo is captured for the backend endpoint.
- D-17: plan + email from rv_user cookie (decoded via Plan 03's decodeSessionUser); plan NAME resolved by fetching /api/v1/plans server-side and finding the entry with `plan.code === planId`. **The planId value in rv_user is kept fresh by Plan 03's proxy (re-issues rv_user with `decodePlanIdFromJwt(access_token)` on every refresh rotation) + Plan 07's force-refresh trigger on /pay/success.**
- D-18: server-side detection — `getSession()` is already used by NavbarApp (Plan 02). Dashboard reuses the same helper and follows the same `dynamic = 'force-dynamic'` pattern (inherited from (app) layout).
- D-25: sign-out POST /api/auth/logout, then navigate to /.

The (app) layout already wraps the page with NavbarApp (Plan 02). This plan adds the page body only.

Backend `/api/v1/plans` response shape (from Plan 05 / Phase 3 03-07):
```ts
type Plan = { code: string; name: string; is_system: boolean; ...; offers: PlanOffer[] };
```
We need only `code` + `name` for the lookup.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: DashboardCard component — email + plan + single CTA</name>
  <files>landing/src/components/app/dashboard-card.tsx</files>
  <read_first>
    - landing/src/components/ui/card.tsx (Plan 02)
    - landing/src/components/app/tier-badge.tsx (Plan 02)
    - landing/src/components/ui/button.tsx
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Page-Specific Layout Contracts — /dashboard)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-15, D-16)
    - landing/src/lib/constants.ts (SUPPORT.telegram)
    - landing/src/i18n/navigation.ts
  </read_first>
  <action>
    Create landing/src/components/app/dashboard-card.tsx as a Server Component:
    ```tsx
    import { getTranslations } from "next-intl/server";
    import { Link } from "@/i18n/navigation";
    import { Card, CardContent } from "@/components/ui/card";
    import { TierBadge } from "@/components/app/tier-badge";
    import { buttonVariants } from "@/components/ui/button";
    import { SUPPORT } from "@/lib/constants";

    type Props = {
      email: string;
      planCode: string;          // "free" | "pro" | other system codes
      planDisplayName: string;   // resolved by parent from /api/v1/plans lookup
    };

    export async function DashboardCard({ email, planCode, planDisplayName }: Props) {
      const t = await getTranslations("dashboard");
      const tPlan = await getTranslations("dashboard.plan");
      const isPro = planCode === "pro";
      return (
        <Card className="max-w-2xl">
          <CardContent>
            <dl className="flex flex-col gap-4">
              <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-1">
                <dt className="text-sm font-medium text-muted-foreground">{t("email")}</dt>
                <dd className="text-base text-foreground">{email || "—"}</dd>
              </div>
              <div className="border-t border-border-subtle" />
              <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-1">
                <dt className="text-sm font-medium text-muted-foreground">{t("plan.label")}</dt>
                <dd className="flex items-center gap-2">
                  <TierBadge tier={isPro ? "pro" : "free"} label={isPro ? tPlan("pro") : tPlan("free")} />
                  <span className="text-sm text-foreground">{planDisplayName}</span>
                </dd>
              </div>
            </dl>
            <div className="mt-6">
              {isPro ? (
                <a
                  href={SUPPORT.telegram}
                  target="_blank"
                  rel="noopener noreferrer"
                  className={buttonVariants({ size: "lg" }) + " w-full"}
                >
                  {t("cta.manage")}
                </a>
              ) : (
                <Link href="/pricing" className={buttonVariants({ size: "lg" }) + " w-full"}>
                  {t("cta.getPro")}
                </Link>
              )}
            </div>
          </CardContent>
        </Card>
      );
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'TierBadge' landing/src/components/app/dashboard-card.tsx` returns at least 1 match
    - `grep -n 'isPro' landing/src/components/app/dashboard-card.tsx` returns at least 2 matches (CTA branch + badge tier)
    - `grep -n 'SUPPORT.telegram' landing/src/components/app/dashboard-card.tsx` returns 1 match
    - `grep -n 'href="/pricing"\|href={"/pricing"}' landing/src/components/app/dashboard-card.tsx` returns 1 match (Free user CTA)
    - `grep -n 'target="_blank"' landing/src/components/app/dashboard-card.tsx` returns 1 match (Telegram link)
    - `grep -n 'rel="noopener noreferrer"' landing/src/components/app/dashboard-card.tsx` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>DashboardCard renders email, plan badge with display name, and a single CTA that swaps based on isPro. Free → /pricing link; Pro → Telegram support link (with target=_blank + rel=noopener).</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: SignOutButton client component with confirmation dialog</name>
  <files>landing/src/app/[locale]/(app)/dashboard/signout-button.tsx</files>
  <read_first>
    - landing/src/components/ui/button.tsx
    - landing/src/components/ui/sheet.tsx (existing base-ui Dialog wrapper)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Destructive confirmation pattern)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-25)
    - landing/src/i18n/navigation.ts
  </read_first>
  <action>
    Create landing/src/app/[locale]/(app)/dashboard/signout-button.tsx:
    ```tsx
    "use client";
    import { useState, useTransition } from "react";
    import { useTranslations } from "next-intl";
    import { useRouter } from "@/i18n/navigation";
    import { Dialog } from "@base-ui/react/dialog";
    import { Button } from "@/components/ui/button";
    import { LogOut } from "lucide-react";

    export function SignOutButton() {
      const t = useTranslations("auth.signOut.confirm");
      const tDash = useTranslations("dashboard");
      const router = useRouter();
      const [open, setOpen] = useState(false);
      const [pending, start] = useTransition();

      async function performSignOut() {
        try {
          await fetch("/api/auth/logout", { method: "POST", credentials: "same-origin" });
        } catch {
          // Plan 03's logout clears cookies even on backend failure; still navigate.
        }
        start(() => {
          router.replace("/");
          router.refresh();
        });
      }

      return (
        <Dialog.Root open={open} onOpenChange={setOpen}>
          <Dialog.Trigger
            render={
              <Button variant="ghost" size="sm" className="text-muted-foreground hover:text-destructive">
                <LogOut className="h-4 w-4" />
                <span>{tDash("signOut")}</span>
              </Button>
            }
          />
          <Dialog.Portal>
            <Dialog.Backdrop className="fixed inset-0 z-50 bg-black/50 backdrop-blur" />
            <Dialog.Popup className="fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2 rounded-[var(--radius-xl)] border border-border-subtle bg-surface-elevated p-6 w-[90vw] max-w-md">
              <Dialog.Title className="text-2xl font-semibold font-heading">{t("title")}</Dialog.Title>
              <Dialog.Description className="mt-2 text-sm text-muted-foreground">{t("body")}</Dialog.Description>
              <div className="mt-6 flex justify-end gap-2">
                <Dialog.Close
                  render={<Button variant="ghost">{t("cancel")}</Button>}
                />
                <Button variant="destructive" disabled={pending} onClick={performSignOut}>{t("confirm")}</Button>
              </div>
            </Dialog.Popup>
          </Dialog.Portal>
        </Dialog.Root>
      );
    }
    ```

    Note: If the installed `@base-ui/react` version (1.4.1) doesn't expose `Dialog` exactly at `@base-ui/react/dialog`, check the existing `landing/src/components/ui/sheet.tsx` for the actual import path and mirror that. The Sheet wrapper likely uses `@base-ui/react/dialog` already.
  </action>
  <acceptance_criteria>
    - `grep -n '"use client"' landing/src/app/\[locale\]/\(app\)/dashboard/signout-button.tsx` returns 1 match
    - `grep -n "/api/auth/logout" landing/src/app/\[locale\]/\(app\)/dashboard/signout-button.tsx` returns 1 match
    - `grep -n 'method: "POST"' landing/src/app/\[locale\]/\(app\)/dashboard/signout-button.tsx` returns 1 match
    - `grep -n 'router.replace("/")\|router.replace(`/`)' landing/src/app/\[locale\]/\(app\)/dashboard/signout-button.tsx` returns 1 match
    - `grep -n 'Dialog.Root\|Dialog.Trigger\|Dialog.Popup' landing/src/app/\[locale\]/\(app\)/dashboard/signout-button.tsx` returns at least 3 matches
    - `grep -n 'destructive' landing/src/app/\[locale\]/\(app\)/dashboard/signout-button.tsx` returns at least 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>Sign-out button opens a destructive confirmation dialog; Confirm fires POST /api/auth/logout and navigates to /.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: /dashboard page — server-gated, resolves plan name, renders DashboardCard + SignOutButton</name>
  <files>landing/src/app/[locale]/(app)/dashboard/page.tsx</files>
  <read_first>
    - landing/src/lib/session.ts (Plan 02 — getSession)
    - landing/src/lib/plans.ts (Plan 05 — fetchPlans)
    - landing/src/lib/locale-currency.ts (Plan 05)
    - landing/src/components/app/dashboard-card.tsx (Task 1)
    - landing/src/app/\[locale\]/\(app\)/dashboard/signout-button.tsx (Task 2)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§/dashboard layout)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-15, D-17, D-18)
    - landing/src/i18n/navigation.ts (redirect)
  </read_first>
  <action>
    Create landing/src/app/[locale]/(app)/dashboard/page.tsx:
    ```tsx
    import { getTranslations } from "next-intl/server";
    import { redirect } from "@/i18n/navigation";
    import { getSession } from "@/lib/session";
    import { fetchPlans } from "@/lib/plans";
    import { currencyForLocale, type Locale } from "@/lib/locale-currency";
    import { DashboardCard } from "@/components/app/dashboard-card";
    import { SignOutButton } from "./signout-button";

    export const dynamic = "force-dynamic";
    export const runtime = "nodejs";

    type Props = { params: Promise<{ locale: string }> };

    export default async function DashboardPage({ params }: Props) {
      const { locale } = await params;
      const session = await getSession();
      if (!session.isAuthed) {
        // Server-side redirect preserves locale and carries next so post-login returns here.
        redirect({ href: { pathname: "/login", query: { next: "/dashboard" } }, locale });
      }

      const t = await getTranslations("dashboard");

      // Resolve plan display name. The rv_user cookie carries planId which is the plan CODE (per Plan 04).
      // Plan 03's proxy keeps rv_user.planId fresh by re-issuing it with decodePlanIdFromJwt(access_token)
      // on every refresh rotation; Plan 07 forces a refresh after a paid invoice so /dashboard reflects Pro
      // immediately after payment.
      const plans = await fetchPlans(currencyForLocale(locale));
      const userPlan = plans.find((p) => p.code === session.planId);
      const planCode = session.planId || "free";
      const planDisplayName = userPlan?.name ?? planCode;

      return (
        <main className="mx-auto max-w-2xl px-6 lg:px-8 py-12 lg:py-16">
          <div className="flex items-center justify-between">
            <h1 className="text-3xl lg:text-4xl font-bold font-heading">{t("heading")}</h1>
            <SignOutButton />
          </div>
          <div className="mt-8">
            <DashboardCard email={session.email} planCode={planCode} planDisplayName={planDisplayName} />
          </div>
        </main>
      );
    }
    ```

    Note on `redirect({...})` from `@/i18n/navigation`: next-intl's `redirect` accepts a `{ href, locale }` object (or sometimes a string). If the installed `next-intl@4.9.1` API requires a different shape, fall back to Next's built-in `redirect` from `next/navigation` and manually prepend the locale: `redirect(`/${locale}/login?next=/dashboard`)`. Verify with the existing `landing/src/i18n/navigation.ts` exports.
  </action>
  <acceptance_criteria>
    - `grep -n 'getSession\|session.isAuthed' landing/src/app/\[locale\]/\(app\)/dashboard/page.tsx` returns at least 2 matches
    - `grep -n 'redirect' landing/src/app/\[locale\]/\(app\)/dashboard/page.tsx` returns at least 1 match
    - `grep -n 'next=/dashboard\|next: "/dashboard"\|"next": "/dashboard"' landing/src/app/\[locale\]/\(app\)/dashboard/page.tsx` returns 1 match
    - `grep -n 'fetchPlans' landing/src/app/\[locale\]/\(app\)/dashboard/page.tsx` returns 1 match
    - `grep -n 'DashboardCard\|SignOutButton' landing/src/app/\[locale\]/\(app\)/dashboard/page.tsx` returns at least 2 matches
    - `grep -n 'force-dynamic' landing/src/app/\[locale\]/\(app\)/dashboard/page.tsx` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>/dashboard server-redirects unauth users to /login?next=/dashboard, resolves plan name via fetchPlans + planId lookup, renders DashboardCard + SignOutButton.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser → /dashboard | Untrusted; server gates on cookie presence |
| rv_user cookie → email render | Untrusted; HTMLly auto-escaped by React |
| sign-out button → /api/auth/logout | Same-origin; cookies carried automatically |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-06-01 | E (Elevation) | unauth visitor reaches /dashboard | mitigate | Task 3 server-side `redirect` before any sensitive render; pure cookie check via `getSession()` |
| T-04-06-02 | I (Info disclosure) | rv_user contains plain email (HMAC-signed but not encrypted) | accept | Email is already user-known; HttpOnly + SameSite=Strict + HMAC integrity (Plan 03) is sufficient. Encryption-at-rest in the cookie is overkill |
| T-04-06-03 | X (XSS via planDisplayName) | plan name from backend rendered in JSX | mitigate | React auto-escapes interpolated strings; only `dangerouslySetInnerHTML` would bypass this. Task 1 uses `{planDisplayName}` directly |
| T-04-06-04 | T (CSRF on sign-out) | POST /api/auth/logout | mitigate | Cookies set with SameSite=Strict (Plan 03) → cross-site POST cannot include them; logout-via-CSRF would just clear the attacker's own session anyway (no useful side effect on the victim) |
| T-04-06-05 | S (Spoofing) | open redirect via `next` query param | mitigate (downstream) | Plan 04's `isSafeNextPath` validates the `next` value at OAuth callback — `/dashboard` is hard-coded literal in Task 3 |
| T-04-06-06 | I (Info disclosure) | Telegram support link leaks user agent to t.me | accept | Standard outbound link; `rel="noopener noreferrer"` set in Task 1 |
| T-04-06-07 | D (DoS) | /dashboard fires /api/v1/plans on every request | mitigate | fetchPlans (Plan 05) uses `next: { tags: ['plans'], revalidate: 600 }`, so it's deduped across all dashboard requests for a given currency. Backend also caches at PAY-12 layer (60s) |
| T-04-06-08 | I (Stale plan_id) | rv_user.planId could be stale (Free shown for Pro user) | mitigate (cross-plan) | Plan 03 re-issues rv_user with `decodePlanIdFromJwt(access_token)` on every refresh-rotation; Plan 07's pay-success handler forces a refresh after `status === "paid"` so the user lands on /dashboard with current planId. /dashboard itself only reads rv_user; freshness ownership lives upstream. |
</threat_model>

<verification>
- WEB-03: GET /<locale>/dashboard without `rv_at` cookie → 307 to /<locale>/login?next=/dashboard (Plan 08 smoke)
- WEB-03: GET /<locale>/dashboard with `rv_at` + `rv_user` cookies → 200 with email + plan + CTA rendered
- WEB-09: Navbar (Plan 02) already provides Dashboard + Sign out links when logged in; this plan adds the dedicated SignOutButton in the page itself for the destructive-confirm pattern
- Sign-out: clicking confirm → POST /api/auth/logout returns 204 → cookies cleared → navigation to /
- TypeScript + build: `cd landing && npm run build` exits 0
</verification>

<success_criteria>
- Server-gated route (WEB-03)
- Email + plan display from rv_user cookie + /api/v1/plans lookup
- Free vs Pro CTA branching per D-15 (Pro link goes to Telegram fallback for Phase 4)
- Sign-out confirmation dialog + POST + redirect (D-25)
- Navbar branching (WEB-09 / SC #6) already validated via Plan 02 — this page exercises it
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-06-dashboard-signout-SUMMARY.md` documenting:
- Server-gating pattern (`getSession()` → `redirect()`)
- Manage Subscription fallback (Telegram support link) + follow-up todo for the backend `/subscription/manage-url` endpoint
- Confirmation dialog markup (base-ui Dialog primitive used here is the same one Plan 04 uses indirectly)
- **W3 deferred scope: billing history (last N invoices) intentionally EXCLUDED from Phase 4 per CONTEXT.md D-15 locked decision. Captured for Phase 7+.**
- D-17 freshness pipeline reference: rv_user.planId stays current via Plan 03's refresh-time JWT decode + Plan 07's post-paid forced refresh. /dashboard is a read-only consumer of that pipeline.
</output>
</content>
</invoke>