---
phase: 04-landing-surfaces
plan: 05
type: execute
wave: 3
depends_on: [04-02, 04-03]
files_modified:
  - landing/src/app/[locale]/(app)/pricing/page.tsx
  - landing/src/app/[locale]/(app)/pricing/pricing-client.tsx
  - landing/src/app/api/revalidate-pricing/route.ts
  - landing/src/components/app/plan-card.tsx
  - landing/src/components/app/currency-switcher.tsx
  - landing/src/lib/plans.ts
  - landing/src/lib/locale-currency.ts
autonomous: true
requirements:
  - WEB-04
  - WEB-08
must_haves:
  truths:
    - "GET /<locale>/pricing renders dynamically from the backend's /api/v1/plans response — no hardcoded prices anywhere in landing/"
    - "Currency is derived from active locale (ru→RUB, en→USD, es→EUR) and overridable via ?currency= which persists to a `pricing_currency` cookie"
    - "Page is statically generated per (locale, currency) tuple with fetch cache tagged 'plans'"
    - "POST /api/revalidate-pricing?secret=<REVALIDATE_SECRET> calls revalidateTag('plans') after constant-time secret comparison; otherwise returns 401"
    - "Empty state (no plans returned) renders an i18n-keyed message, not a crash"
  artifacts:
    - path: "landing/src/app/[locale]/(app)/pricing/page.tsx"
      provides: "Pricing page — server component, ISR with fetch tag 'plans'"
    - path: "landing/src/app/api/revalidate-pricing/route.ts"
      provides: "Webhook receiver — secret-protected, calls revalidateTag('plans')"
      exports: ["POST"]
    - path: "landing/src/components/app/plan-card.tsx"
      provides: "Plan card — name, price, period, features, CTA"
      exports: ["PlanCard"]
    - path: "landing/src/components/app/currency-switcher.tsx"
      provides: "Chip group: USD / EUR / RUB — pushes ?currency= to URL"
      exports: ["CurrencySwitcher"]
    - path: "landing/src/lib/plans.ts"
      provides: "fetchPlans(currency, locale) helper — calls backend with `next: { tags: ['plans'] }`"
      exports: ["fetchPlans", "type Plan", "type PlanOffer"]
    - path: "landing/src/lib/locale-currency.ts"
      provides: "Locale → default currency mapping + currency formatter"
      exports: ["currencyForLocale", "formatPrice"]
  key_links:
    - from: "landing/src/app/[locale]/(app)/pricing/page.tsx"
      to: "landing/src/lib/plans.ts → fetch(`${BACKEND_API_URL}/api/v1/plans?currency=...`)"
      via: "fetchPlans(currency, locale)"
      pattern: "fetchPlans\\("
    - from: "landing/src/app/api/revalidate-pricing/route.ts"
      to: "next/cache → revalidateTag('plans')"
      via: "revalidateTag"
      pattern: "revalidateTag\\([\"']plans[\"']\\)"
    - from: "landing/src/components/app/currency-switcher.tsx"
      to: "?currency= query param + pricing_currency cookie"
      via: "next-intl router.replace + document.cookie"
      pattern: "pricing_currency"
tags: [pricing, isr, plans, currency, i18n]
---

<objective>
Build a fully dynamic /pricing page (WEB-04 + WEB-08) that:
- Renders three locale × currency variants at build/request time: `/ru/pricing?currency=RUB|USD|EUR`, `/en/pricing?currency=USD|EUR|RUB`, `/es/pricing?currency=EUR|USD|RUB` (default currency = D-04 locale-based mapping)
- Fetches plans from `${BACKEND_API_URL}/api/v1/plans?currency=<C>` with `next: { tags: ['plans'] }` so a single tag bust regenerates ALL variants
- Exposes a `POST /api/revalidate-pricing?secret=<REVALIDATE_SECRET>` endpoint that the Go backend's admin write handlers call (D-13/D-14) — constant-time secret compare, then `revalidateTag('plans')`
- Has zero hardcoded prices anywhere in `landing/` source

The page is also the entry point for the checkout flow (Plan 07) — the CTA on each plan card carries `plan`, `period`, `currency` query params that Plan 07's checkout client component consumes.

Purpose: This page is the public face of the Pro tier. WEB-04 ("renders dynamically, ISR with on-demand revalidate") is the single most user-visible Phase 4 deliverable for SC #5. Currency switching is the most-failed test in similar landing pages — get it right server-side or lose conversions in non-USD markets.

Output: A signed-out visitor visits `/<locale>/pricing`, sees pricing in their locale's currency, can toggle currency, and a CTA click forwards to Plan 07's checkout flow. Admin updates a plan in admin-web → fan-out hits /api/revalidate-pricing → all three variants regenerate on next request.
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
@.planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md
@landing/src/lib/env.ts
@landing/src/lib/cookies.ts
@landing/src/components/ui/card.tsx

<interfaces>
<!-- Locked CONTEXT.md decisions -->
- D-04: locale → currency default mapping: ru→RUB, en→USD, es→EUR. `?currency=` query param overrides. Override persists to `pricing_currency` cookie (HttpOnly: NO — this cookie is read by client to render the chip — set as a regular cookie with Path=/; SameSite=Lax; Max-Age=2592000 (30 days); Secure when prod).
- D-13: ISR via `fetch(url, { next: { tags: ['plans'] } })`. Tag bust via `revalidateTag('plans')` regenerates all dependent pages on next request.
- D-14: `POST /api/revalidate-pricing?secret=<REVALIDATE_SECRET>` — constant-time compare; on success revalidate tag.

Backend contract (Phase 3 plan 03-07):
- GET /api/v1/plans?currency=USD|EUR|RUB → 200 with body
  ```json
  {
    "plans": [
      {
        "code": "free",
        "name": "Free",
        "is_system": true,
        "device_limit": 1,
        "monthly_traffic_mb": 5000,
        "server_countries": ["NL","DE"],
        "offers": []
      },
      {
        "code": "pro",
        "name": "Pro",
        "is_system": true,
        "device_limit": 5,
        "monthly_traffic_mb": 0,
        "server_countries": ["NL","DE","US","UK","SG","JP"],
        "offers": [
          { "period": "monthly", "price": 4.99, "currency": "USD", "lava_offer_id": "uuid-xxx" },
          { "period": "yearly",  "price": 49.99, "currency": "USD", "lava_offer_id": "uuid-yyy" }
        ]
      }
    ]
  }
  ```
- Cached server-side at backend with 60s TTL (PAY-12 cache wrapper); Phase 4 adds the LANDING-side ISR tag on top of that.

`landing/src/components/ui/card.tsx` interfaces (created in Plan 02):
```ts
export function Card(props: HTMLAttributes<HTMLDivElement>): JSX.Element;
export function CardTitle(props: HTMLAttributes<HTMLHeadingElement>): JSX.Element;
// ... CardHeader, CardContent, CardFooter
```

`landing/src/components/app/tier-badge.tsx` (created in Plan 02):
```ts
export function TierBadge({ tier: "free" | "pro"; label: string }): JSX.Element;
```
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: fetchPlans helper + locale→currency mapping + currency formatter</name>
  <files>landing/src/lib/plans.ts, landing/src/lib/locale-currency.ts</files>
  <read_first>
    - landing/src/lib/env.ts
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-04, D-13)
    - .planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md (response shape)
  </read_first>
  <action>
    Create landing/src/lib/locale-currency.ts (NOT server-only — usable in client components):
    ```ts
    export type Currency = "USD" | "EUR" | "RUB";
    export type Locale = "ru" | "en" | "es";

    const LOCALE_DEFAULT: Record<Locale, Currency> = { ru: "RUB", en: "USD", es: "EUR" };

    export function currencyForLocale(locale: string, override?: string): Currency {
      const o = (override ?? "").toUpperCase();
      if (o === "USD" || o === "EUR" || o === "RUB") return o;
      return LOCALE_DEFAULT[(locale as Locale)] ?? "USD";
    }

    export function formatPrice(amount: number, currency: Currency, locale: Locale): string {
      const intlLocale = locale === "ru" ? "ru-RU" : locale === "es" ? "es-ES" : "en-US";
      try {
        return new Intl.NumberFormat(intlLocale, { style: "currency", currency, minimumFractionDigits: 0, maximumFractionDigits: 2 }).format(amount);
      } catch {
        // Fallback for unrecognised currency
        return `${amount.toFixed(2)} ${currency}`;
      }
    }
    ```

    Create landing/src/lib/plans.ts (server-only):
    ```ts
    import "server-only";
    import { env } from "./env";
    import type { Currency } from "./locale-currency";

    export type PlanOffer = {
      period: "monthly" | "yearly";
      price: number;
      currency: Currency;
      lava_offer_id: string;
    };

    export type Plan = {
      code: string;
      name: string;
      is_system: boolean;
      device_limit: number;
      monthly_traffic_mb: number;
      server_countries: string[];
      offers: PlanOffer[];
    };

    export async function fetchPlans(currency: Currency): Promise<Plan[]> {
      const url = `${env.BACKEND_API_URL}/api/v1/plans?currency=${currency}`;
      const r = await fetch(url, {
        next: { tags: ["plans"], revalidate: 600 },  // 10-min background refresh + tag bust on admin write
        signal: AbortSignal.timeout(8000),
      });
      if (!r.ok) {
        // Surface empty array → page renders empty state (per UI-SPEC).
        console.warn("[plans] fetch non-ok", { status: r.status, currency });
        return [];
      }
      const json = await r.json().catch(() => ({ plans: [] }));
      const plans: Plan[] = Array.isArray(json?.plans) ? json.plans : [];
      return plans;
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n "currencyForLocale" landing/src/lib/locale-currency.ts` returns 1 match
    - `grep -n '"USD"\|"EUR"\|"RUB"' landing/src/lib/locale-currency.ts` returns at least 3 matches
    - `grep -n 'Intl.NumberFormat' landing/src/lib/locale-currency.ts` returns 1 match
    - `grep -n 'import "server-only"' landing/src/lib/plans.ts` returns 1 match
    - `grep -n "tags:.*\\[.*['\"]plans['\"].*\\]" landing/src/lib/plans.ts` returns 1 match
    - `grep -n 'env.BACKEND_API_URL.*/api/v1/plans' landing/src/lib/plans.ts` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>fetchPlans hits the backend with `next: { tags: ['plans'] }`, locale-currency.ts maps ru→RUB/en→USD/es→EUR and exposes a typed formatter.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: /pricing page (server component) + PlanCard + CurrencySwitcher</name>
  <files>landing/src/app/[locale]/(app)/pricing/page.tsx, landing/src/app/[locale]/(app)/pricing/pricing-client.tsx, landing/src/components/app/plan-card.tsx, landing/src/components/app/currency-switcher.tsx</files>
  <read_first>
    - landing/src/lib/plans.ts (Task 1)
    - landing/src/lib/locale-currency.ts (Task 1)
    - landing/src/lib/session.ts (Plan 02 — getSession to detect logged-in user for CTA wording)
    - landing/src/components/ui/card.tsx (Plan 02)
    - landing/src/components/app/tier-badge.tsx (Plan 02)
    - landing/src/i18n/navigation.ts
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Page-Specific Layout Contracts — /pricing; §Copywriting Contract — pricing.*)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-04, D-13, D-19, D-20)
  </read_first>
  <action>
    Create landing/src/app/[locale]/(app)/pricing/page.tsx (Server Component):
    ```tsx
    import { getTranslations } from "next-intl/server";
    import { fetchPlans } from "@/lib/plans";
    import { currencyForLocale, type Locale } from "@/lib/locale-currency";
    import { getSession } from "@/lib/session";
    import { PlanCard } from "@/components/app/plan-card";
    import { CurrencySwitcher } from "@/components/app/currency-switcher";
    import { PricingClient } from "./pricing-client";

    // Use ISR semantics — the route is force-dynamic at the (app) layout level (Plan 02),
    // but we override it back to static-with-tag here because /pricing is a public page
    // that benefits from caching across users.
    export const dynamic = "force-static";
    export const revalidate = 600;

    type Props = {
      params: Promise<{ locale: string }>;
      searchParams: Promise<{ currency?: string; plan?: string; period?: string; checkout?: string }>;
    };

    export default async function PricingPage({ params, searchParams }: Props) {
      const { locale } = await params;
      const sp = await searchParams;
      const currency = currencyForLocale(locale, sp.currency);
      const t = await getTranslations("pricing");
      const tPlan = await getTranslations("dashboard.plan");
      const plans = await fetchPlans(currency);
      const session = await getSession();
      const currentPlanId = session.isAuthed ? session.planId : null;

      return (
        <main className="mx-auto max-w-6xl px-6 lg:px-8 py-16 lg:py-24">
          <section className="text-center">
            <h1 className="text-5xl lg:text-6xl font-bold font-heading text-foreground">{t("heading")}</h1>
            <p className="mt-4 text-base text-muted-foreground">{t("subhead")}</p>
            <div className="mt-8 flex justify-center">
              <CurrencySwitcher locale={locale as Locale} active={currency} />
            </div>
          </section>

          {plans.length === 0 ? (
            <div className="mt-16 mx-auto max-w-md text-center">
              <h2 className="text-2xl font-semibold font-heading">{t("empty.heading")}</h2>
            </div>
          ) : (
            <section className="mt-16 grid grid-cols-1 md:grid-cols-2 gap-6 max-w-4xl mx-auto">
              {plans.map((p) => (
                <PlanCard
                  key={p.code}
                  plan={p}
                  locale={locale as Locale}
                  currency={currency}
                  isCurrent={currentPlanId === p.code || (currentPlanId && p.is_system && p.code === currentPlanId)}
                  isAuthed={session.isAuthed}
                  freeLabel={tPlan("free")}
                  proLabel={tPlan("pro")}
                />
              ))}
            </section>
          )}

          {/* The client component below detects checkout=auto and triggers Plan 07's auto-checkout. */}
          <PricingClient locale={locale} plan={sp.plan} period={sp.period} checkout={sp.checkout} currency={currency} />
        </main>
      );
    }
    ```

    Create landing/src/components/app/currency-switcher.tsx (Client Component — needs router push):
    ```tsx
    "use client";
    import { useTransition } from "react";
    import { useRouter, usePathname } from "@/i18n/navigation";
    import type { Currency, Locale } from "@/lib/locale-currency";

    const CHOICES: Currency[] = ["USD", "EUR", "RUB"];

    type Props = { locale: Locale; active: Currency };
    export function CurrencySwitcher({ active }: Props) {
      const router = useRouter();
      const pathname = usePathname();
      const [pending, start] = useTransition();
      return (
        <div role="group" aria-label="Currency" className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-surface/40 p-1 backdrop-blur">
          {CHOICES.map((c) => {
            const isActive = c === active;
            return (
              <button
                key={c}
                type="button"
                disabled={pending || isActive}
                onClick={() => {
                  // Set a 30-day pricing_currency cookie so server-side reads default to this on next visit.
                  document.cookie = `pricing_currency=${c}; Max-Age=2592000; Path=/; SameSite=Lax${location.protocol === "https:" ? "; Secure" : ""}`;
                  start(() => {
                    const u = new URL(window.location.href);
                    u.searchParams.set("currency", c);
                    router.replace(`${pathname}?${u.searchParams.toString()}`);
                  });
                }}
                className={`rounded-full px-3 py-1 font-mono text-xs uppercase tracking-wider transition ${
                  isActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:text-foreground"
                }`}
                aria-current={isActive ? "true" : undefined}
              >
                {c}
              </button>
            );
          })}
        </div>
      );
    }
    ```

    Create landing/src/components/app/plan-card.tsx (Server Component — receives all i18n labels as props):
    ```tsx
    import { Link } from "@/i18n/navigation";
    import { Card, CardTitle, CardContent, CardFooter } from "@/components/ui/card";
    import { TierBadge } from "@/components/app/tier-badge";
    import { buttonVariants } from "@/components/ui/button";
    import { formatPrice, type Currency, type Locale } from "@/lib/locale-currency";
    import type { Plan } from "@/lib/plans";
    import { getTranslations } from "next-intl/server";
    import { Check } from "lucide-react";

    type Props = { plan: Plan; locale: Locale; currency: Currency; isAuthed: boolean; isCurrent: boolean; freeLabel: string; proLabel: string };

    export async function PlanCard({ plan, locale, currency, isAuthed, isCurrent, freeLabel, proLabel }: Props) {
      const t = await getTranslations("pricing");
      const isPro = plan.code === "pro";
      const monthlyOffer = plan.offers.find((o) => o.period === "monthly");
      const features = [
        plan.device_limit > 0 ? `${plan.device_limit} devices` : "Unlimited devices",
        plan.monthly_traffic_mb > 0 ? `${(plan.monthly_traffic_mb / 1024).toFixed(0)} GB / month` : "Unlimited traffic",
        `${plan.server_countries.length} countries`,
      ];

      // Build CTA target.
      let ctaHref: string;
      if (isCurrent) ctaHref = "";
      else if (!isAuthed && isPro) ctaHref = `/login?next=/pricing&plan=pro&period=monthly&currency=${currency}`;
      else if (isAuthed && isPro) ctaHref = `/pricing?plan=pro&period=monthly&currency=${currency}&checkout=auto`;
      else ctaHref = "";  // Free plan has no CTA when free is current; Free with no session is informational

      return (
        <Card className={`relative ${isPro ? "border-border ring-1 ring-accent-glow/20" : ""}`}>
          <div className="flex items-start justify-between">
            <CardTitle>{plan.name}</CardTitle>
            {isPro && <TierBadge tier="pro" label={proLabel} />}
            {!isPro && <TierBadge tier="free" label={freeLabel} />}
          </div>
          <div className="mt-4">
            {monthlyOffer ? (
              <p className="text-5xl font-bold font-heading">
                {formatPrice(monthlyOffer.price, currency, locale)}
                <span className="ml-2 text-base font-normal text-muted-foreground">{t("period.monthly")}</span>
              </p>
            ) : (
              <p className="text-5xl font-bold font-heading">{formatPrice(0, currency, locale)}</p>
            )}
          </div>
          <CardContent>
            <ul className="mt-6 flex flex-col gap-2">
              {features.map((f) => (
                <li key={f} className="flex items-center gap-2 text-sm text-foreground">
                  <Check className="h-4 w-4 text-accent" />
                  <span>{f}</span>
                </li>
              ))}
            </ul>
          </CardContent>
          <CardFooter className="mt-6">
            {isCurrent ? (
              <button disabled className="w-full opacity-50 cursor-not-allowed rounded-[var(--radius-md)] border border-border bg-surface px-4 py-2 text-sm">
                {t("cta.current")}
              </button>
            ) : isPro ? (
              <Link href={ctaHref} className={buttonVariants({ size: "lg" }) + " w-full"}>
                {t("cta.getPro")}
              </Link>
            ) : null}
          </CardFooter>
        </Card>
      );
    }
    ```

    Create landing/src/app/[locale]/(app)/pricing/pricing-client.tsx (Client Component — owned by Plan 07; for THIS plan, render a minimal placeholder that mounts and waits for Plan 07's expansion):
    ```tsx
    "use client";
    type Props = { locale: string; plan?: string; period?: string; checkout?: string; currency: string };
    export function PricingClient(_: Props) {
      // Plan 07 implements the auto-checkout effect here. For Plan 05 this is a no-op so the page renders.
      return null;
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'fetchPlans' landing/src/app/\[locale\]/\(app\)/pricing/page.tsx` returns 1 match
    - `grep -n 'currencyForLocale' landing/src/app/\[locale\]/\(app\)/pricing/page.tsx` returns 1 match
    - `grep -n 'getSession' landing/src/app/\[locale\]/\(app\)/pricing/page.tsx` returns 1 match
    - `grep -n 'PlanCard\|CurrencySwitcher' landing/src/app/\[locale\]/\(app\)/pricing/page.tsx` returns at least 2 matches
    - `grep -n 'pricing_currency' landing/src/components/app/currency-switcher.tsx` returns 1 match
    - `grep -n 'SameSite=Lax' landing/src/components/app/currency-switcher.tsx` returns 1 match
    - `grep -rn 'data-price=\|"price":\s*[0-9]\|"\$[0-9]\|"€[0-9]\|"₽[0-9]' landing/src/` returns 0 matches (no hardcoded prices)
    - `grep -n 'formatPrice' landing/src/components/app/plan-card.tsx` returns at least 1 match
    - `grep -n 'checkout=auto' landing/src/components/app/plan-card.tsx` returns 1 match (logged-in CTA target)
    - `grep -n 'next=/pricing.*plan=pro' landing/src/components/app/plan-card.tsx` returns 1 match (logged-out CTA target)
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>/pricing page renders dynamically from backend /api/v1/plans, currency switcher updates URL + cookie, plan card CTA carries the right query string for logged-in vs logged-out users, and no hardcoded prices appear anywhere in the source.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: /api/revalidate-pricing route — secret-protected revalidateTag('plans')</name>
  <files>landing/src/app/api/revalidate-pricing/route.ts</files>
  <read_first>
    - landing/src/lib/env.ts (REVALIDATE_SECRET)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-13, D-14)
  </read_first>
  <action>
    Create landing/src/app/api/revalidate-pricing/route.ts:
    ```ts
    import { NextRequest, NextResponse } from "next/server";
    import { revalidateTag } from "next/cache";
    import { timingSafeEqual } from "node:crypto";
    import { env } from "@/lib/env";

    export const dynamic = "force-dynamic";
    export const runtime = "nodejs";

    function safeEq(a: string, b: string): boolean {
      const ab = Buffer.from(a);
      const bb = Buffer.from(b);
      if (ab.length !== bb.length) return false;
      return timingSafeEqual(ab, bb);
    }

    export async function POST(req: NextRequest) {
      const url = new URL(req.url);
      const provided = url.searchParams.get("secret") ?? "";
      if (!safeEq(provided, env.REVALIDATE_SECRET)) {
        // Slow down probe attacks with a 1-byte delay equivalent: just return 401.
        return NextResponse.json({ error: "unauthorized" }, { status: 401 });
      }
      revalidateTag("plans");
      return NextResponse.json({ revalidated: true, tag: "plans" }, { status: 200 });
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'revalidateTag("plans")' landing/src/app/api/revalidate-pricing/route.ts` returns 1 match
    - `grep -n 'timingSafeEqual' landing/src/app/api/revalidate-pricing/route.ts` returns 1 match
    - `grep -n 'env.REVALIDATE_SECRET' landing/src/app/api/revalidate-pricing/route.ts` returns 1 match
    - `grep -n 'status: 401\|status: 200' landing/src/app/api/revalidate-pricing/route.ts` returns at least 2 matches
    - `grep -n 'runtime = "nodejs"' landing/src/app/api/revalidate-pricing/route.ts` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=test123 APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>POST /api/revalidate-pricing?secret=<wrong> returns 401; POST /api/revalidate-pricing?secret=<right> returns 200 + busts the 'plans' cache tag.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| public visitor → /pricing | Untrusted; all data is read-only from backend |
| backend admin write → /api/revalidate-pricing | Shared secret over HTTPS; SSRF surface |
| client form → backend /plans | Mediated by Node proxy (Plan 03); CSRF n/a for GET |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-05-01 | E (Elevation) | /api/revalidate-pricing | mitigate | Task 3 constant-time compares `?secret=` against `env.REVALIDATE_SECRET`; mismatched secret returns 401 with no other side effects |
| T-04-05-02 | I (Info disclosure) | REVALIDATE_SECRET in client bundle | mitigate | Plan 01's `env.ts` is `server-only`; importing into a client component fails the build. Secret never reaches the browser |
| T-04-05-03 | D (DoS) | unauthenticated /pricing scrapers | accept | Backend's /api/v1/plans is public + cached (PAY-12); landing-side ISR (revalidate: 600s) means at most 1 backend call per 10 min per (locale, currency). Tag bust still cheap. Rate-limit handled at nginx for landing |
| T-04-05-04 | T (Tampering) | plan tampering (client submits modified plan id at checkout) | mitigate (downstream) | Plan 07's checkout uses `plan` + `period` query params to call backend `/checkout`. Backend trusts ONLY its own `plans` + `plan_offers` tables — client-supplied values are looked up; tampered codes return 4xx. Phase 3 PAY-08 closure |
| T-04-05-05 | T (Tampering) | malicious `?currency=` value | mitigate | Task 1 `currencyForLocale` allow-lists USD/EUR/RUB; any other value falls back to locale default |
| T-04-05-06 | I (Info disclosure) | server_countries leak of internal infra | accept | server_countries is intentionally public (PAY-12); Phase 3 already vets this list. Phase 4 just forwards it |
| T-04-05-07 | S (Spoofing) | open admin tag-bust callbacks | mitigate | Task 3 requires the secret as a query param. NOTE: query params can land in access logs — Phase-future hardening should move the secret to a request header. For Phase 4, accepted given the per-bust rate is low and HTTPS is enforced |
| T-04-05-08 | I (Info disclosure) | pricing_currency cookie | accept | Plain string ("USD"/"EUR"/"RUB"); not sensitive |
</threat_model>

<verification>
- SC #5: `grep -rn '\\$[0-9]\\|€[0-9]\\|₽[0-9]\\|"price":\\s*[0-9]' landing/src/` returns 0 matches (no hardcoded prices)
- SC #5 (continued): Three locale variants (`/ru/pricing`, `/en/pricing`, `/es/pricing`) build to separate ISR variants → `curl -I http://localhost:3000/ru/pricing` and friends each return 200 with proper Cache-Control
- Revalidate: POST /api/revalidate-pricing?secret=<env value> → 200; with wrong secret → 401 (Plan 08 smoke)
- WEB-04: Currency switcher click updates `?currency=` and persists `pricing_currency` cookie (Plan 08 Playwright assertion)
- Build: `npm run build` exits 0 with all env vars provided
</verification>

<success_criteria>
- /pricing renders dynamically from /api/v1/plans (WEB-04)
- Locale → currency mapping ru→RUB / en→USD / es→EUR works (D-04)
- ?currency= overrides + persists to cookie (D-04)
- revalidateTag('plans') wired behind a constant-time secret check (D-13/D-14)
- Empty state renders cleanly when backend returns no plans
- No hardcoded prices anywhere in landing/
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-SUMMARY.md` documenting:
- Static-with-tag mode chosen for /pricing (overrides parent (app) layout's force-dynamic)
- pricing_currency cookie attributes (non-HttpOnly, SameSite=Lax, 30-day)
- Phase 3 follow-up: backend admin handlers in `server/api/internal/handler/plans_admin.go` need to POST to `${APP_URL}/api/revalidate-pricing?secret=${REVALIDATE_SECRET}` after each successful write. Capture as `/gsd-note` (operator follow-up).
- Phase-future hardening flag: secret-in-query-param vs secret-in-header
</output>
