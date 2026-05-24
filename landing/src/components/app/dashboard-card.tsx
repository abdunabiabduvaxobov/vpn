import { getTranslations } from "next-intl/server";

import { Link } from "@/i18n/navigation";
import { TierBadge } from "@/components/app/tier-badge";
import { Card, CardContent } from "@/components/ui/card";
import { buttonVariants } from "@/components/ui/button";
import { SUPPORT } from "@/lib/constants";

/**
 * DashboardCard — the single content surface on /<locale>/dashboard.
 *
 * Renders the signed-in user's email + current plan (with a TierBadge) and a
 * single context-aware CTA:
 *   - Free user → next-intl Link to /pricing (carries locale prefix)
 *   - Pro user  → outbound link to the Telegram support handle (D-16 fallback
 *     until backend `/api/v1/subscription/manage-url` lands). Outbound link
 *     gets `target="_blank"` + `rel="noopener noreferrer"` per
 *     T-04-06-06 mitigation.
 *
 * Server-component-friendly: pulls translations from next-intl/server, takes
 * the email + plan info as props from the parent server page (no client
 * hydration). The plan display name is resolved by the caller from the
 * `/api/v1/plans` lookup (planId → plan.name); we receive both the raw code
 * (drives the badge tier branch) and the display name (renders next to the
 * badge).
 */

type Props = {
  email: string;
  planCode: string; // "free" | "pro" | other system codes
  planDisplayName: string; // resolved by parent from /api/v1/plans lookup
};

export async function DashboardCard({
  email,
  planCode,
  planDisplayName,
}: Props) {
  const t = await getTranslations("dashboard");
  const tPlan = await getTranslations("dashboard.plan");
  const isPro = planCode === "pro";
  return (
    <Card className="max-w-2xl">
      <CardContent>
        <dl className="flex flex-col gap-4">
          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-1">
            <dt className="text-sm font-medium text-muted-foreground">
              {t("email")}
            </dt>
            <dd className="text-base text-foreground">{email || "—"}</dd>
          </div>
          <div className="border-t border-border-subtle" />
          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-1">
            <dt className="text-sm font-medium text-muted-foreground">
              {t("plan.label")}
            </dt>
            <dd className="flex items-center gap-2">
              <TierBadge
                tier={isPro ? "pro" : "free"}
                label={isPro ? tPlan("pro") : tPlan("free")}
              />
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
            <Link
              href="/pricing"
              className={buttonVariants({ size: "lg" }) + " w-full"}
            >
              {t("cta.getPro")}
            </Link>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
