import { getTranslations } from "next-intl/server";

import { redirect } from "@/i18n/navigation";
import { getSession } from "@/lib/session";
import { fetchPlans } from "@/lib/plans";
import { currencyForLocale } from "@/lib/locale-currency";
import { DashboardCard } from "@/components/app/dashboard-card";

import { SignOutButton } from "./signout-button";

/**
 * /<locale>/dashboard — protected user landing surface (WEB-03).
 *
 * Server-side gated: if the `rv_at` HttpOnly cookie is absent (or
 * decodes to a non-authed session), redirect to /<locale>/login with
 * `?next=/dashboard` so post-login lands the user back here. The
 * redirect MUST happen before any sensitive render — getSession() is the
 * single gate (T-04-06-01 mitigation).
 *
 * Display data:
 *   - email + planId come from the HMAC-verified rv_user cookie (Plan 03
 *     decodeSessionUser → getSession()).
 *   - planId is kept fresh by Plan 03's proxy (re-issues rv_user with
 *     decodePlanIdFromJwt(access_token) on every refresh rotation) and by
 *     Plan 07's force-refresh trigger after a paid invoice. /dashboard is
 *     a pure read-only consumer of that pipeline (D-17 closure).
 *   - plan display NAME is looked up server-side via fetchPlans against
 *     the active locale's currency. The lookup tolerates a missing match
 *     (display falls back to the raw planId string).
 *
 * The (app) layout already sets `dynamic = 'force-dynamic'`; we restate
 * it here for clarity since the rest of this file's surface area is
 * cookie + locale dependent. `runtime = 'nodejs'` matches the rest of
 * Phase 4 (Plan 03's proxy + Plan 01's standalone build invariant).
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type Props = { params: Promise<{ locale: string }> };

export default async function DashboardPage({ params }: Props) {
  const { locale } = await params;
  const session = await getSession();
  if (!session.isAuthed) {
    // Server-side redirect preserves locale and carries `next` so
    // post-login returns here. T-04-06-01 (E) closure.
    // next-intl's redirect is typed `(...) => never`, but we still return
    // it explicitly so TS narrows `session` to the authed branch below
    // without depending on inferred never-throw flow analysis.
    return redirect({
      href: { pathname: "/login", query: { next: "/dashboard" } },
      locale,
    });
  }

  const t = await getTranslations("dashboard");

  // Resolve plan display name. The rv_user cookie carries planId which is
  // the plan CODE (per Plan 04 OAuth callback). Plan 03's proxy keeps
  // rv_user.planId fresh via decodePlanIdFromJwt(access_token) on every
  // refresh rotation; Plan 07 forces a refresh after a paid invoice so
  // /dashboard reflects Pro immediately after payment.
  const plans = await fetchPlans(currencyForLocale(locale));
  const planCode = session.planId || "free";
  const userPlan = plans.find((p) => p.code === planCode);
  const planDisplayName = userPlan?.name ?? planCode;

  return (
    <main className="mx-auto max-w-2xl px-6 lg:px-8 py-12 lg:py-16">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl lg:text-4xl font-bold font-heading">
          {t("heading")}
        </h1>
        <SignOutButton />
      </div>
      <div className="mt-8">
        <DashboardCard
          email={session.email}
          planCode={planCode}
          planDisplayName={planDisplayName}
        />
      </div>
    </main>
  );
}

