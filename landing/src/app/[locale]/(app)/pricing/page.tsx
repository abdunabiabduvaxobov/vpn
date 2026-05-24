import { getTranslations } from "next-intl/server";

import { PricingClient } from "./pricing-client";
import { CurrencySwitcher } from "@/components/app/currency-switcher";
import { PlanCard } from "@/components/app/plan-card";
import { getSession } from "@/lib/session";
import { currencyForLocale, type Locale } from "@/lib/locale-currency";
import { fetchPlans } from "@/lib/plans";

/**
 * /pricing — Phase 4 WEB-04 + WEB-08, Plan 04-05.
 *
 * IMPORTANT: This file intentionally inherits the (app) layout's request-time
 * render mode (`dynamic = "force-dynamic"` set in Plan 02). We do NOT pin
 * this page to build-time static rendering — doing so would freeze the
 * cookie jar to its build-time (empty) value and a logged-in Pro user would
 * appear logged-out, see the "Get Pro" CTA, and could trigger a duplicate
 * checkout (T-04-05-09 closure).
 *
 * Cache freshness is handled at the fetch() layer instead — see fetchPlans
 * in `lib/plans.ts`:
 *
 *   fetch(url, { next: { tags: ["plans"], revalidate: 600 } })
 *
 * revalidateTag("plans") in /api/revalidate-pricing (Task 3) still busts that
 * fetch entry across all renders, so D-13's "statically generated, tag-bust
 * on admin write" intent is preserved at the data-cache layer rather than the
 * page-render layer. A single backend call services up to 10 minutes of
 * traffic per (locale, currency) variant; admin writes invalidate it within
 * one HTTP round-trip.
 */

export const runtime = "nodejs";

type SearchParams = {
  currency?: string;
  plan?: string;
  period?: string;
  checkout?: string;
};

type Props = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<SearchParams>;
};

export default async function PricingPage({ params, searchParams }: Props) {
  const { locale } = await params;
  const sp = await searchParams;

  // T-04-05-05: currencyForLocale allow-lists USD/EUR/RUB and falls back to
  // the locale default for any other value. Untrusted `?currency=` input is
  // sanitised before it ever influences the backend fetch URL.
  const currency = currencyForLocale(locale, sp.currency);

  const t = await getTranslations("pricing");
  const tPlan = await getTranslations("dashboard.plan");

  // Fetch in parallel — session read is local cookie jar (cheap), plans fetch
  // hits the backend with the 10-min fetch-tag cache (also cheap on a hit).
  const [plans, session] = await Promise.all([
    fetchPlans(currency),
    getSession(),
  ]);

  const currentPlanCode = session.isAuthed ? session.planId : null;

  return (
    <main className="mx-auto max-w-6xl px-6 py-16 lg:px-8 lg:py-24">
      <section className="text-center">
        <h1 className="text-5xl font-bold font-heading text-foreground lg:text-6xl">
          {t("heading")}
        </h1>
        <p className="mt-4 text-base text-muted-foreground">{t("subhead")}</p>
        <div className="mt-8 flex justify-center">
          <CurrencySwitcher locale={locale as Locale} active={currency} />
        </div>
      </section>

      {plans.length === 0 ? (
        <div className="mx-auto mt-16 max-w-md text-center">
          <h2 className="text-2xl font-semibold font-heading">
            {t("empty.heading")}
          </h2>
        </div>
      ) : (
        <section className="mx-auto mt-16 grid max-w-4xl grid-cols-1 gap-6 md:grid-cols-2">
          {plans.map((p) => (
            <PlanCard
              key={p.code}
              plan={p}
              locale={locale as Locale}
              currency={currency}
              isAuthed={session.isAuthed}
              isCurrent={Boolean(currentPlanCode && currentPlanCode === p.code)}
              freeLabel={tPlan("free")}
              proLabel={tPlan("pro")}
            />
          ))}
        </section>
      )}

      {/*
        Plan 07 (checkout) implements the auto-checkout effect inside
        PricingClient. For Plan 05 the client component is a no-op stub so
        the page renders and ISR caches work end-to-end before checkout
        logic lands.
      */}
      <PricingClient
        locale={locale}
        plan={sp.plan}
        period={sp.period}
        checkout={sp.checkout}
        currency={currency}
      />
    </main>
  );
}
