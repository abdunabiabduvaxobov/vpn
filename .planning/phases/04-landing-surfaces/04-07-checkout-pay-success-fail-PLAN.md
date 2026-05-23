---
phase: 04-landing-surfaces
plan: 07
type: execute
wave: 4
depends_on: [04-04, 04-05]
files_modified:
  - landing/src/app/[locale]/(app)/pricing/pricing-client.tsx
  - landing/src/app/[locale]/(app)/pay/success/page.tsx
  - landing/src/app/[locale]/(app)/pay/success/poll-client.tsx
  - landing/src/app/[locale]/(app)/pay/fail/page.tsx
  - landing/src/components/app/payment-status-card.tsx
  - landing/src/components/app/payment-fail-card.tsx
autonomous: true
requirements:
  - WEB-05
  - WEB-06
  - WEB-07
must_haves:
  truths:
    - "A logged-in visitor on /pricing who clicks 'Get Pro' triggers POST /api/v1/checkout in a single round-trip and redirects to the returned lava.top paymentUrl"
    - "A logged-out visitor clicking 'Get Pro' is redirected to /<locale>/login?next=/pricing&plan=pro&period=monthly&currency=<C>; after sign-in returns to /<locale>/pricing?plan=pro&period=monthly&currency=<C>&checkout=auto and the auto-checkout fires immediately"
    - "/pay/success?invoiceId=X polls GET /api/v1/invoices/{id} every 2s — polls 1-5 use the cheap path, polls 6+ add ?escalate=true"
    - "When status flips to 'paid', the page shows 'Pro is active!' and a Continue button → /<locale>/dashboard; polling stops"
    - "After 30s without 'paid', the page shows 'Still processing your payment…' with a Refresh button + Telegram support link; polling stops"
    - "If status flips to 'failed', the page redirects to /<locale>/pay/fail?invoiceId=X&reason=<r>"
    - "/pay/fail renders i18n-aware messaging with a 'Try again' CTA back to /pricing (preserving plan/period/currency)"
  artifacts:
    - path: "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx"
      provides: "Client component on /pricing — when ?checkout=auto, POST /api/v1/checkout and redirect to paymentUrl"
      exports: ["PricingClient"]
    - path: "landing/src/app/[locale]/(app)/pay/success/page.tsx"
      provides: "/pay/success — auth-gated server page that hosts PollClient"
    - path: "landing/src/app/[locale]/(app)/pay/success/poll-client.tsx"
      provides: "Polling logic — 2s interval, escalate at poll 6, 30s timeout, transitions to active/timeout/failed states"
      exports: ["PollClient"]
    - path: "landing/src/app/[locale]/(app)/pay/fail/page.tsx"
      provides: "/pay/fail — reads ?reason= and renders matching copy"
    - path: "landing/src/components/app/payment-status-card.tsx"
      provides: "Three render states: processing, active, takingLonger"
      exports: ["PaymentStatusCard"]
    - path: "landing/src/components/app/payment-fail-card.tsx"
      provides: "Fail page card — icon, heading, body per reason, Try again + Support"
      exports: ["PaymentFailCard"]
  key_links:
    - from: "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx"
      to: "POST /api/v1/checkout (via Plan 03 proxy)"
      via: "fetch('/api/v1/checkout', { method: 'POST', body: {plan_code, periodicity, currency} })"
      pattern: "/api/v1/checkout"
    - from: "landing/src/app/[locale]/(app)/pay/success/poll-client.tsx"
      to: "GET /api/v1/invoices/{id}[?escalate=true]"
      via: "setInterval 2s"
      pattern: "/api/v1/invoices"
    - from: "landing/src/app/[locale]/(app)/pay/fail/page.tsx"
      to: "/<locale>/pricing?plan=...&period=...&currency=..."
      via: "Link href"
      pattern: "Link.*pricing"
tags: [checkout, payments, polling, lava, isr]
---

<objective>
Wire the entire money flow: logged-out → /pricing → /login → /pricing (auto-checkout) → lava.top → /pay/success → Pro active. Plus the failure path /pay/fail. This is the SC #2, SC #3, SC #4 closure plan — the heart of the value proposition the milestone is built around.

Specifically:
- WEB-05: "Get Pro" on /pricing POSTs /checkout and redirects to lava.top; logged-out auto-redirects to /login with the right `next=` and resumes after sign-in.
- WEB-06: /pay/success polls /invoices/{id} for up to 30s with 2s cadence + escalation at poll 6, shows "we'll email you" after timeout.
- WEB-07: /pay/fail shows friendly retry CTA back to /pricing.

Purpose: This is where money becomes possible. Plan 04 puts identity in place; Plan 05 displays prices; Plan 06 confirms the user; **this plan** is the only thing that converts intent into a paid subscription. Every concrete number (2s poll, escalate at poll 6, 30s timeout) is locked in CONTEXT D-21 and must not be changed.

Output: A logged-in user clicks "Get Pro" → lava.top opens in a new tab → user pays → lava redirects to /pay/success → within ~2s of webhook arrival the page shows "Pro is active!" → Continue → /dashboard with the Pro badge.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-UI-SPEC.md
@.planning/phases/04-landing-surfaces/04-04-login-oauth-callback-PLAN.md
@.planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-PLAN.md
@.planning/phases/04-landing-surfaces/04-06-dashboard-signout-PLAN.md
@.planning/phases/03-lava-top-plans-catalog/03-05-checkout-cancel-invoices-admin-lava-proxy-SUMMARY.md
@landing/src/lib/session.ts
@landing/src/lib/locale-currency.ts
@landing/src/lib/constants.ts
@landing/src/components/ui/card.tsx
@landing/src/i18n/navigation.ts

<interfaces>
<!-- Locked CONTEXT.md decisions -->
- D-19: logged-out → /login?next=/pricing&plan=pro&period=monthly&currency=<C>. After sign-in → /pricing?plan=pro&period=monthly&currency=<C>&checkout=auto. /pricing detects checkout=auto and POSTs /checkout immediately.
- D-20: logged-in click → single POST /checkout → redirect to paymentUrl (no extra screens).
- D-21: polling cadence — 2s interval, polls 1-5 use cheap path, polls 6+ add ?escalate=true, 30s total timeout, statuses "paid" / "pending" / "failed" / "cancelled".
- D-22: i18n message keys — `pay.success.processing` / `.active` / `.takingLonger.heading` / `.takingLonger.body` / `.refresh` / `.contactSupport` / `.continue`.
- D-23: /pay/fail copy — title constant, body varies by `?reason=` (default/declined/cancelled), primary CTA "Try again" → /pricing, secondary "Contact support" → Telegram.

Backend contracts (Phase 3 plan 03-05):
- POST /api/v1/checkout — body `{plan_code: "pro", periodicity: "monthly" | "yearly", currency: "USD"|"EUR"|"RUB"}` → 200 `{payment_url: string, invoice_id: string}`. Auth required (Bearer via Plan 03 proxy).
- GET /api/v1/invoices/:id[?escalate=true] — returns `{id, status: "pending"|"paid"|"failed"|"cancelled", plan_code, ...}`. Auth required. 404 if invoice doesn't belong to caller (ownership check).
- lava.top URL TTL ~24h per CLAUDE.md lava constraints.

Status mapping (status string → UI state):
- "paid" → active
- "pending" → processing (continue polling)
- "failed" → redirect to /pay/fail
- "cancelled" → redirect to /pay/fail?reason=cancelled

Note: backend may return lowercased OR uppercased status (per 03-05 SUMMARY: `mapLavaStatusToLocal` normalises both casings). Client polling MUST normalise to lowercase before comparing.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: PricingClient — replace stub with checkout-flow client. Handles checkout=auto on /pricing.</name>
  <files>landing/src/app/[locale]/(app)/pricing/pricing-client.tsx</files>
  <read_first>
    - landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx (created as stub in Plan 05)
    - landing/src/app/\[locale\]/\(app\)/pricing/page.tsx (Plan 05 — passes plan/period/checkout/currency props)
    - landing/src/components/app/plan-card.tsx (Plan 05 — CTA href format)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-19, D-20)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (errors.* keys for failure toasts)
    - landing/src/i18n/navigation.ts
  </read_first>
  <action>
    REPLACE landing/src/app/[locale]/(app)/pricing/pricing-client.tsx with a real client component:
    ```tsx
    "use client";
    import { useEffect, useRef, useState } from "react";
    import { useTranslations } from "next-intl";
    import { useRouter } from "@/i18n/navigation";

    type Props = { locale: string; plan?: string; period?: string; checkout?: string; currency: string };

    export function PricingClient({ plan, period, checkout, currency }: Props) {
      const t = useTranslations("errors");
      const router = useRouter();
      const fired = useRef(false);
      const [error, setError] = useState<string | null>(null);

      useEffect(() => {
        if (fired.current) return;
        if (checkout !== "auto" || !plan || !period) return;
        fired.current = true;
        (async () => {
          try {
            const r = await fetch("/api/v1/checkout", {
              method: "POST",
              headers: { "Content-Type": "application/json" },
              credentials: "same-origin",
              body: JSON.stringify({ plan_code: plan, periodicity: period, currency }),
            });
            if (r.status === 401) {
              // Session expired — bounce back to /login with the same params so the round-trip resumes.
              const next = `/pricing?plan=${plan}&period=${period}&currency=${currency}&checkout=auto`;
              router.replace(`/login?next=${encodeURIComponent(next)}`);
              return;
            }
            if (!r.ok) { setError(t("network")); return; }
            const json = await r.json().catch(() => null);
            const url = json?.payment_url;
            if (typeof url !== "string" || !/^https:\/\/(gate\.|app\.|pay\.)?lava\.top\//.test(url)) {
              setError(t("network"));
              return;
            }
            window.location.href = url;
          } catch {
            setError(t("network"));
          }
        })();
      }, [checkout, plan, period, currency, router, t]);

      if (error) {
        return (
          <div role="alert" className="mx-auto mt-8 max-w-md rounded-[var(--radius-md)] border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
            {error}
          </div>
        );
      }
      return null;
    }
    ```

    Whitelist check (`^https:\/\/(gate\.|app\.|pay\.)?lava\.top\/`) prevents the backend from accidentally returning a non-lava URL that the client would then follow (open-redirect-via-payment-provider hardening — even though the backend is trusted, defense in depth here is one regex). Confirm with `landing/src/app/api` proxy that lava URLs always live on `*.lava.top`.

    Also: update landing/src/components/app/plan-card.tsx (created in Plan 05) so the LOGGED-IN Pro CTA points to the SAME query string Plan 05 already encoded — verify no change needed. If Plan 05 used `/pricing?plan=pro&period=monthly&currency=<C>&checkout=auto`, this plan's PricingClient handles it; otherwise normalise.
  </action>
  <acceptance_criteria>
    - `grep -n '"use client"' landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx` returns 1 match
    - `grep -n '/api/v1/checkout' landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx` returns 1 match
    - `grep -n 'method: "POST"' landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx` returns 1 match
    - `grep -n 'plan_code\|periodicity' landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx` returns at least 2 matches
    - `grep -n 'lava\.top' landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx` returns 1 match (URL whitelist)
    - `grep -n 'status === 401\|r.status === 401' landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx` returns 1 match
    - `grep -n 'checkout !== "auto"\|checkout === "auto"' landing/src/app/\[locale\]/\(app\)/pricing/pricing-client.tsx` returns at least 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>/pricing?checkout=auto&plan=pro&period=monthly&currency=USD triggers POST /api/v1/checkout once, lava.top URL whitelisted, 401 bounces back to /login with preserved query.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: /pay/success — PollClient (2s + escalate + 30s timeout) + PaymentStatusCard</name>
  <files>landing/src/app/[locale]/(app)/pay/success/page.tsx, landing/src/app/[locale]/(app)/pay/success/poll-client.tsx, landing/src/components/app/payment-status-card.tsx</files>
  <read_first>
    - landing/src/lib/session.ts (Plan 02)
    - landing/src/components/ui/card.tsx
    - landing/src/components/ui/button.tsx
    - landing/src/lib/constants.ts (SUPPORT.telegram)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-21, D-22)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§/pay/success + §Copywriting Contract)
    - .planning/phases/03-lava-top-plans-catalog/03-05-checkout-cancel-invoices-admin-lava-proxy-SUMMARY.md (status semantics, escalate=true)
    - landing/src/i18n/navigation.ts
  </read_first>
  <action>
    Create landing/src/components/app/payment-status-card.tsx (Server-renderable; receives state as prop from PollClient — split into a presentation component so PollClient can swap by state):
    ```tsx
    "use client";
    import { useTranslations } from "next-intl";
    import { Card, CardContent } from "@/components/ui/card";
    import { buttonVariants } from "@/components/ui/button";
    import { Link } from "@/i18n/navigation";
    import { Loader2, Check, AlertCircle } from "lucide-react";
    import { SUPPORT } from "@/lib/constants";

    type Props =
      | { state: "processing"; invoiceId: string }
      | { state: "active"; invoiceId: string }
      | { state: "takingLonger"; invoiceId: string; onRefresh: () => void };

    export function PaymentStatusCard(p: Props) {
      const t = useTranslations("pay.success");
      if (p.state === "processing") {
        return (
          <Card className="text-center">
            <CardContent className="flex flex-col items-center gap-4 py-12">
              <Loader2 className="h-12 w-12 text-accent animate-spin" aria-hidden />
              <h2 className="text-2xl font-semibold font-heading">{t("processing")}</h2>
              <p className="font-mono text-sm text-subtle-foreground">{p.invoiceId}</p>
            </CardContent>
          </Card>
        );
      }
      if (p.state === "active") {
        return (
          <Card className="text-center">
            <CardContent className="flex flex-col items-center gap-4 py-12">
              <Check className="h-12 w-12 text-success" aria-hidden />
              <h2 className="text-2xl font-semibold font-heading">{t("active")}</h2>
              <Link href="/dashboard" className={buttonVariants({ size: "lg" }) + " mt-4"}>{t("continue")}</Link>
            </CardContent>
          </Card>
        );
      }
      return (
        <Card className="text-center">
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <AlertCircle className="h-12 w-12 text-muted-foreground" aria-hidden />
            <h2 className="text-2xl font-semibold font-heading">{t("takingLonger.heading")}</h2>
            <p className="text-sm text-muted-foreground max-w-sm">{t("takingLonger.body")}</p>
            <p className="font-mono text-sm text-subtle-foreground mt-2">{p.invoiceId}</p>
            <div className="mt-2 flex flex-col gap-2 w-full max-w-xs">
              <button onClick={p.onRefresh} className={buttonVariants({ variant: "outline", size: "lg" })}>{t("refresh")}</button>
              <a href={SUPPORT.telegram} target="_blank" rel="noopener noreferrer" className="text-sm text-subtle-foreground hover:text-muted-foreground">{t("contactSupport")}</a>
            </div>
          </CardContent>
        </Card>
      );
    }
    ```

    Create landing/src/app/[locale]/(app)/pay/success/poll-client.tsx:
    ```tsx
    "use client";
    import { useEffect, useRef, useState } from "react";
    import { useRouter } from "@/i18n/navigation";
    import { PaymentStatusCard } from "@/components/app/payment-status-card";

    type Props = { invoiceId: string; locale: string };

    const INTERVAL_MS = 2000;
    const ESCALATE_AFTER_POLL = 6;     // polls 1-5 cheap; from poll 6 add ?escalate=true (D-21)
    const TIMEOUT_MS = 30000;           // 30s overall (D-21)

    export function PollClient({ invoiceId, locale }: Props) {
      const router = useRouter();
      const [view, setView] = useState<"processing" | "active" | "takingLonger">("processing");
      const pollNo = useRef(0);
      const timerRef = useRef<number | null>(null);
      const timeoutRef = useRef<number | null>(null);
      const stopped = useRef(false);

      function stop() {
        stopped.current = true;
        if (timerRef.current) window.clearInterval(timerRef.current);
        if (timeoutRef.current) window.clearTimeout(timeoutRef.current);
      }

      async function pollOnce() {
        if (stopped.current) return;
        pollNo.current += 1;
        const useEscalate = pollNo.current >= ESCALATE_AFTER_POLL;
        const url = `/api/v1/invoices/${encodeURIComponent(invoiceId)}${useEscalate ? "?escalate=true" : ""}`;
        try {
          const r = await fetch(url, { credentials: "same-origin" });
          if (r.status === 401) { stop(); router.replace("/login?next=/dashboard"); return; }
          if (r.status === 404) { stop(); router.replace(`/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=default`); return; }
          if (!r.ok) return; // transient — keep polling
          const json = await r.json().catch(() => null);
          const status = (json?.status ?? "").toString().toLowerCase();
          if (status === "paid") { stop(); setView("active"); return; }
          if (status === "failed") { stop(); router.replace(`/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=declined`); return; }
          if (status === "cancelled") { stop(); router.replace(`/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=cancelled`); return; }
          // "pending" or unknown → keep polling
        } catch {
          // network blip — keep polling
        }
      }

      useEffect(() => {
        // Kick first poll immediately so user sees a transition at ~poll 1.
        pollOnce();
        timerRef.current = window.setInterval(pollOnce, INTERVAL_MS);
        timeoutRef.current = window.setTimeout(() => {
          if (stopped.current) return;
          if (timerRef.current) window.clearInterval(timerRef.current);
          setView("takingLonger");
        }, TIMEOUT_MS);
        return () => stop();
      }, []);

      function refresh() {
        // Re-poll once on manual refresh after timeout. Doesn't restart the timer — single shot.
        (async () => {
          const url = `/api/v1/invoices/${encodeURIComponent(invoiceId)}?escalate=true`;
          try {
            const r = await fetch(url, { credentials: "same-origin" });
            if (r.ok) {
              const json = await r.json().catch(() => null);
              const status = (json?.status ?? "").toString().toLowerCase();
              if (status === "paid") setView("active");
              else if (status === "failed") router.replace(`/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=declined`);
              else if (status === "cancelled") router.replace(`/pay/fail?invoiceId=${encodeURIComponent(invoiceId)}&reason=cancelled`);
            }
          } catch {}
        })();
      }

      if (view === "processing") return <PaymentStatusCard state="processing" invoiceId={invoiceId} />;
      if (view === "active") return <PaymentStatusCard state="active" invoiceId={invoiceId} />;
      return <PaymentStatusCard state="takingLonger" invoiceId={invoiceId} onRefresh={refresh} />;
    }
    ```

    Create landing/src/app/[locale]/(app)/pay/success/page.tsx (Server Component — auth-gated):
    ```tsx
    import { redirect } from "@/i18n/navigation";
    import { getSession } from "@/lib/session";
    import { PollClient } from "./poll-client";

    export const dynamic = "force-dynamic";
    export const runtime = "nodejs";

    type Props = { params: Promise<{ locale: string }>; searchParams: Promise<{ invoiceId?: string }> };

    export default async function PaySuccessPage({ params, searchParams }: Props) {
      const { locale } = await params;
      const sp = await searchParams;
      const session = await getSession();
      if (!session.isAuthed) {
        const next = `/pay/success${sp.invoiceId ? `?invoiceId=${encodeURIComponent(sp.invoiceId)}` : ""}`;
        redirect({ href: { pathname: "/login", query: { next } }, locale });
      }
      if (!sp.invoiceId) {
        // Missing param → bounce to dashboard.
        redirect({ href: "/dashboard", locale });
      }
      return (
        <main className="mx-auto max-w-md px-6 py-16">
          <PollClient invoiceId={sp.invoiceId!} locale={locale} />
        </main>
      );
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'INTERVAL_MS = 2000\|INTERVAL_MS=2000' landing/src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx` returns 1 match (2s cadence)
    - `grep -n 'ESCALATE_AFTER_POLL = 6\|ESCALATE_AFTER_POLL=6' landing/src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx` returns 1 match
    - `grep -n 'TIMEOUT_MS = 30000\|TIMEOUT_MS=30000' landing/src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx` returns 1 match
    - `grep -n 'escalate=true' landing/src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx` returns at least 2 matches (poll + refresh)
    - `grep -n "/api/v1/invoices/" landing/src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx` returns at least 2 matches
    - `grep -n '"paid"\|"failed"\|"cancelled"\|"pending"' landing/src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx` returns at least 4 matches (all statuses handled)
    - `grep -n 'toLowerCase' landing/src/app/\[locale\]/\(app\)/pay/success/poll-client.tsx` returns at least 1 match (casing normalisation)
    - `grep -n 'getSession' landing/src/app/\[locale\]/\(app\)/pay/success/page.tsx` returns 1 match
    - `grep -n 'PaymentStatusCard' landing/src/components/app/payment-status-card.tsx` returns at least 1 match
    - `grep -n 'pay.success.takingLonger\|pay.success.processing\|pay.success.active' landing/src/components/app/payment-status-card.tsx` returns at least 3 matches (or via translation calls)
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>/pay/success?invoiceId=X polls per D-21 contract, transitions to active/timeout/failed correctly, auth-gates the page, and renders the three UI states with the exact i18n keys.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: /pay/fail page + PaymentFailCard (reason-aware)</name>
  <files>landing/src/app/[locale]/(app)/pay/fail/page.tsx, landing/src/components/app/payment-fail-card.tsx</files>
  <read_first>
    - landing/src/components/ui/card.tsx
    - landing/src/lib/constants.ts (SUPPORT.telegram)
    - landing/src/i18n/navigation.ts
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-23 — /pay/fail content)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§/pay/fail layout + copywriting)
  </read_first>
  <action>
    Create landing/src/components/app/payment-fail-card.tsx (Server Component):
    ```tsx
    import { getTranslations } from "next-intl/server";
    import { Link } from "@/i18n/navigation";
    import { Card, CardContent } from "@/components/ui/card";
    import { buttonVariants } from "@/components/ui/button";
    import { X } from "lucide-react";
    import { SUPPORT } from "@/lib/constants";

    type Reason = "default" | "declined" | "cancelled";

    type Props = { reason: Reason; tryAgainHref: string };
    const REASON_KEY: Record<Reason, "body.default" | "body.declined" | "body.cancelled"> = {
      default: "body.default", declined: "body.declined", cancelled: "body.cancelled",
    };

    export async function PaymentFailCard({ reason, tryAgainHref }: Props) {
      const t = await getTranslations("pay.fail");
      return (
        <Card className="text-center">
          <CardContent className="flex flex-col items-center gap-4 py-12">
            <X className="h-12 w-12 text-destructive" aria-hidden />
            <h1 className="text-3xl lg:text-4xl font-bold font-heading">{t("title")}</h1>
            <p className="text-base text-muted-foreground max-w-sm">{t(REASON_KEY[reason])}</p>
            <div className="mt-2 flex flex-col gap-2 w-full max-w-xs">
              <Link href={tryAgainHref} className={buttonVariants({ size: "lg" })}>{t("tryAgain")}</Link>
              <a href={SUPPORT.telegram} target="_blank" rel="noopener noreferrer" className="text-sm text-subtle-foreground hover:text-muted-foreground">{t("contactSupport")}</a>
            </div>
          </CardContent>
        </Card>
      );
    }
    ```

    Create landing/src/app/[locale]/(app)/pay/fail/page.tsx:
    ```tsx
    import { PaymentFailCard } from "@/components/app/payment-fail-card";

    export const dynamic = "force-dynamic";

    type Reason = "default" | "declined" | "cancelled";
    function safeReason(r: string | undefined): Reason {
      return r === "declined" || r === "cancelled" ? r : "default";
    }

    type Props = { params: Promise<{ locale: string }>; searchParams: Promise<{ reason?: string; plan?: string; period?: string; currency?: string }> };

    export default async function PayFailPage({ params, searchParams }: Props) {
      const { locale } = await params;
      const sp = await searchParams;
      const reason = safeReason(sp.reason);

      // Build try-again target — preserve plan/period/currency if present, else go to bare /pricing.
      const qs = new URLSearchParams();
      if (sp.plan) qs.set("plan", sp.plan);
      if (sp.period) qs.set("period", sp.period);
      if (sp.currency) qs.set("currency", sp.currency);
      const tryAgainHref = qs.toString() ? `/pricing?${qs.toString()}` : "/pricing";

      return (
        <main className="mx-auto max-w-md px-6 py-16">
          <PaymentFailCard reason={reason} tryAgainHref={tryAgainHref} />
        </main>
      );
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'pay.fail' landing/src/components/app/payment-fail-card.tsx` returns at least 1 match (translation namespace)
    - `grep -n 'body.default\|body.declined\|body.cancelled' landing/src/components/app/payment-fail-card.tsx` returns at least 3 matches (all three reasons)
    - `grep -n 'SUPPORT.telegram' landing/src/components/app/payment-fail-card.tsx` returns 1 match
    - `grep -n 'safeReason\|"declined"\|"cancelled"\|"default"' landing/src/app/\[locale\]/\(app\)/pay/fail/page.tsx` returns at least 3 matches
    - `grep -n 'tryAgainHref\|/pricing' landing/src/app/\[locale\]/\(app\)/pay/fail/page.tsx` returns at least 2 matches
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>/pay/fail?reason={default|declined|cancelled} renders the matching body copy and a Try again link back to /pricing with preserved plan/period/currency.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| client → /api/v1/checkout | Authenticated via Plan 03 proxy; client-supplied plan_code/periodicity/currency must be validated server-side |
| client polling → /api/v1/invoices | Authenticated; backend enforces ownership (returns 404 on mismatch per 03-05) |
| /pay/success?invoiceId=X URL | Untrusted invoiceId from query — backend ownership check is the gate |
| lava.top → landing redirect | Untrusted querystring on /pay/success / /pay/fail; we DO NOT trust ?reason= to label success — only the /invoices status decides |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-07-01 | T (Open redirect) | payment_url from /checkout response | mitigate | Task 1 regex-whitelists `^https:\/\/(gate\.|app\.|pay\.)?lava\.top\/` before `window.location.href = url`. Even though backend is trusted, defence-in-depth eliminates the post-trust-breach surface |
| T-04-07-02 | T (Plan tampering) | client submits modified `plan_code`/`periodicity` | mitigate (downstream) | Backend trusts only its `plans`/`plan_offers` tables; PAY-08 (Phase 3) ensures tier is derived from lava `offerId`, not client metadata. Landing forwards as-is |
| T-04-07-03 | I (Info disclosure) | invoiceId enumeration on /pay/success | mitigate | Backend `GET /invoices/:id` returns 404 (NOT 403) on ownership mismatch (per 03-05 SUMMARY) — indistinguishable from "doesn't exist". Polling that gets 404 stops + redirects to /pay/fail |
| T-04-07-04 | D (DoS) | aggressive polling against /invoices | mitigate | Hard 30s timeout; polls cap at 15 (30s/2s); ESCALATE only triggers after poll 6 to amortise lava-side calls. Backend already cached + escalate has a per-IP rate limit (HOTFIX-03) |
| T-04-07-05 | S (Spoofing) | malicious `?reason=` query on /pay/fail | mitigate | Task 3 `safeReason()` allow-lists the three values; anything else falls back to "default" |
| T-04-07-06 | I (Info disclosure) | invoiceId in browser history | accept | invoiceId is per-user (ownership-checked) — even if shared, the other user can't read it. Standard payment-flow UX |
| T-04-07-07 | X (XSS via translation) | i18n strings rendered | mitigate | React auto-escapes; translation values come from compiled JSON files (not user input). Task 2 + Task 3 use `{t(...)}` syntax only |
| T-04-07-08 | T (Tampering) | client-side 30s timeout bypass | accept | Timeout is purely UX (when to show "we'll email you"); status comes from server only. Bypassing → just longer polling, which the rate-limit catches |
| T-04-07-09 | T (CSRF on checkout POST) | /api/v1/checkout | mitigate | Plan 03 proxy uses cookies → Bearer translation; cookies are SameSite=Strict so cross-site POST cannot include them. The endpoint also requires JSON body which a form-CSRF can't produce without Content-Type tricks |
| T-04-07-10 | E (Elevation) | unauth user reaches /pay/success | mitigate | Task 2 page server-gates on `getSession()` and redirects to /login?next=/pay/success?invoiceId=... |
| T-04-07-11 | I (Info disclosure) | escalate=true triggers lava call without user gate | mitigate | Phase 3 03-05 ownership check happens before escalate. Backend won't call lava on a non-owned invoice |
</threat_model>

<verification>
- SC #2: logged-in click → POST /api/v1/checkout → 200 paymentUrl → window.location.href redirect (one HTTP round-trip). Plan 08 smoke captures the network log.
- SC #3: logged-out click → /login?next=/pricing&plan=pro&period=monthly&currency=USD → sign-in completes → /pricing?plan=pro&period=monthly&currency=USD&checkout=auto → POST /checkout fires once. Plan 08 Playwright.
- SC #4: stub backend responses to confirm polling cadence (2s / escalate at poll 6 / 30s timeout). Plan 08 Playwright with `route.continue` interception.
- TypeScript + build: `cd landing && npm run build` exits 0.
</verification>

<success_criteria>
- WEB-05 closure: checkout flow works logged-in (direct) + logged-out (deep-link → resume after sign-in)
- WEB-06 closure: polling per D-21 contract (interval, escalate, timeout)
- WEB-07 closure: /pay/fail with reason-aware copy + retry CTA preserving plan/period/currency
- Open-redirect defence on payment_url whitelist
- Status casing normalised; all four invoice states handled
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-07-checkout-pay-success-fail-SUMMARY.md` documenting:
- payment_url whitelist regex chosen
- Exact polling numbers (D-21 verbatim)
- Status casing normalisation (lowercase) — mirrors backend mapLavaStatusToLocal
- /pay/fail reason allow-list (default / declined / cancelled)
</output>
