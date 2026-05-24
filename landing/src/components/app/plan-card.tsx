import { Check } from "lucide-react";
import { getTranslations } from "next-intl/server";

import { Link } from "@/i18n/navigation";
import {
  Card,
  CardContent,
  CardFooter,
  CardTitle,
} from "@/components/ui/card";
import { TierBadge } from "@/components/app/tier-badge";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { formatPrice, type Currency, type Locale } from "@/lib/locale-currency";
import type { Plan } from "@/lib/plans";

/**
 * PlanCard — server component used in the /pricing grid. Receives every
 * dependency it needs as props (plan data from `fetchPlans`, currency from
 * the page-resolved active currency, isAuthed/isCurrent from `getSession`)
 * so it can fully render server-side without any client hydration cost.
 *
 * CTA branching (T-04-05-04 closure — backend re-validates the plan code
 * downstream regardless of what we ship in the query string):
 *
 *   - isCurrent       → "Current plan" disabled button. No href. Never
 *                       trigger a duplicate checkout.
 *   - !isAuthed + Pro → "Get Pro" → /login?next=/pricing&plan=pro&...
 *                       After login, the OAuth callback (Plan 04) honours
 *                       `next` so the user lands back at /pricing where the
 *                       client component reads `?checkout=auto` (Plan 07).
 *   - isAuthed  + Pro → "Get Pro" → /pricing?plan=pro&period=monthly&
 *                       currency=<C>&checkout=auto. Plan 07's <PricingClient/>
 *                       detects checkout=auto and POSTs /checkout.
 *   - Free plan       → No CTA (free is the default tier; nothing to upgrade
 *                       to within itself).
 */

type Props = {
  plan: Plan;
  locale: Locale;
  currency: Currency;
  isAuthed: boolean;
  isCurrent: boolean;
  freeLabel: string;
  proLabel: string;
};

export async function PlanCard({
  plan,
  locale,
  currency,
  isAuthed,
  isCurrent,
  freeLabel,
  proLabel,
}: Props) {
  const t = await getTranslations("pricing");
  const isPro = plan.code === "pro";
  const monthlyOffer = plan.offers.find((o) => o.period === "monthly");

  // Feature bullets — derived from the backend's plan record so admin-side
  // changes (device_limit, monthly_traffic_mb, server_countries) flow through
  // without a code deploy. UI-SPEC §pricing — Check icon + label, 8px gap.
  const features: string[] = [
    plan.device_limit > 0
      ? `${plan.device_limit} devices`
      : "Unlimited devices",
    plan.monthly_traffic_mb > 0
      ? `${(plan.monthly_traffic_mb / 1024).toFixed(0)} GB / month`
      : "Unlimited traffic",
    `${plan.server_countries.length} countries`,
  ];

  // CTA target. Empty string means no link will be rendered.
  let ctaHref = "";
  if (!isCurrent) {
    if (isPro) {
      ctaHref = isAuthed
        ? `/pricing?plan=pro&period=monthly&currency=${currency}&checkout=auto`
        : `/login?next=/pricing&plan=pro&period=monthly&currency=${currency}`;
    }
    // Free plan: no upgrade target within itself.
  }

  return (
    <Card
      className={cn(
        "relative",
        isPro ? "ring-1 ring-accent-glow/20" : undefined,
      )}
    >
      <div className="flex items-start justify-between">
        <CardTitle>{plan.name}</CardTitle>
        <TierBadge tier={isPro ? "pro" : "free"} label={isPro ? proLabel : freeLabel} />
      </div>

      <div className="mt-4">
        {monthlyOffer ? (
          <p className="text-5xl font-bold font-heading">
            {formatPrice(monthlyOffer.price, currency, locale)}
            <span className="ml-2 text-base font-normal text-muted-foreground">
              {t("period.monthly")}
            </span>
          </p>
        ) : (
          <p className="text-5xl font-bold font-heading">
            {formatPrice(0, currency, locale)}
          </p>
        )}
      </div>

      <CardContent>
        <ul className="mt-6 flex flex-col gap-2">
          {features.map((f) => (
            <li
              key={f}
              className="flex items-center gap-2 text-sm text-foreground"
            >
              <Check className="h-4 w-4 text-accent" />
              <span>{f}</span>
            </li>
          ))}
        </ul>
      </CardContent>

      <CardFooter className="mt-6">
        {isCurrent ? (
          <button
            disabled
            className="w-full cursor-not-allowed rounded-[var(--radius-md)] border border-border bg-surface px-4 py-2 text-sm opacity-50"
          >
            {t("cta.current")}
          </button>
        ) : isPro && ctaHref ? (
          <Link
            href={ctaHref}
            className={cn(buttonVariants({ size: "lg" }), "w-full")}
          >
            {t("cta.getPro")}
          </Link>
        ) : null}
      </CardFooter>
    </Card>
  );
}
