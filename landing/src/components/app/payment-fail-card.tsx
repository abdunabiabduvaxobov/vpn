/**
 * PaymentFailCard — server component for /<locale>/pay/fail (Plan 04-07 / WEB-07).
 *
 * Reason-aware body copy (CONTEXT D-23):
 *   - default   → "Your card was not charged. Please try again…"
 *   - declined  → "Your bank declined the charge…"
 *   - cancelled → "You cancelled the payment…"
 *
 * The Try-again CTA target is computed by the page (PaymentFailCard receives
 * the resolved href) so it can preserve plan/period/currency from the URL the
 * lava.top redirect handed us. Falls back to bare /pricing if those params
 * weren't carried through.
 *
 * Telegram support link uses `rel="noopener noreferrer"` so the popup cannot
 * read window.opener (closes the analog of T-04-06-06 for this page).
 */

import { X } from "lucide-react";
import { getTranslations } from "next-intl/server";

import { Link } from "@/i18n/navigation";
import { Card, CardContent } from "@/components/ui/card";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { SUPPORT } from "@/lib/constants";

export type FailReason = "default" | "declined" | "cancelled";

const REASON_KEY: Record<FailReason, "body.default" | "body.declined" | "body.cancelled"> = {
  default: "body.default",
  declined: "body.declined",
  cancelled: "body.cancelled",
};

type Props = {
  reason: FailReason;
  tryAgainHref: string;
};

export async function PaymentFailCard({ reason, tryAgainHref }: Props) {
  const t = await getTranslations("pay.fail");
  return (
    <Card className="text-center">
      <CardContent className="flex flex-col items-center gap-4 py-12">
        <X className="h-12 w-12 text-destructive" aria-hidden />
        <h1 className="text-3xl font-bold font-heading lg:text-4xl">
          {t("title")}
        </h1>
        <p className="max-w-sm text-base text-muted-foreground">
          {t(REASON_KEY[reason])}
        </p>
        <div className="mt-2 flex w-full max-w-xs flex-col gap-2">
          <Link
            href={tryAgainHref}
            className={cn(buttonVariants({ size: "lg" }))}
          >
            {t("tryAgain")}
          </Link>
          <a
            href={SUPPORT.telegram}
            target="_blank"
            rel="noopener noreferrer"
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            {t("contactSupport")}
          </a>
        </div>
      </CardContent>
    </Card>
  );
}
