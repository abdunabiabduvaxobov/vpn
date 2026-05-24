"use client";

/**
 * PaymentStatusCard — presentation component for /pay/success.
 *
 * Three render states (controlled by `state` discriminator):
 *
 *   - "processing"   → spinner + "Activating your Pro subscription…" + invoice id.
 *                      Used while polling /api/v1/invoices/:id (CONTEXT D-22).
 *   - "active"       → success check + "Pro is active!" + Continue → /dashboard.
 *                      Final state — reached when status flips to "paid" AND the
 *                      forced refresh has rotated rv_user.planId to "pro".
 *   - "takingLonger" → muted icon + "Still processing your payment…" + Refresh
 *                      button + Telegram support link. Shown after the 30s
 *                      poll timeout (D-21) without seeing "paid".
 *
 * Pure presentation. The poll/refresh state machine lives in PollClient — this
 * component only renders what it's told. The split keeps the card swap-friendly
 * (easy to render any of the three states in isolation during tests/storybook).
 *
 * i18n: every visible string comes from the `pay.success.*` namespace populated
 * in Plan 01 (D-22 keys). Specific keys consumed below:
 *   - pay.success.processing
 *   - pay.success.active
 *   - pay.success.continue
 *   - pay.success.takingLonger.heading
 *   - pay.success.takingLonger.body
 *   - pay.success.refresh
 *   - pay.success.contactSupport
 */

import { useTranslations } from "next-intl";
import { Loader2, Check, AlertCircle } from "lucide-react";

import { Link } from "@/i18n/navigation";
import { Card, CardContent } from "@/components/ui/card";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
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
          <h2 className="text-2xl font-semibold font-heading">
            {t("processing")}
          </h2>
          <p className="font-mono text-sm text-muted-foreground">
            {p.invoiceId}
          </p>
        </CardContent>
      </Card>
    );
  }

  if (p.state === "active") {
    return (
      <Card className="text-center">
        <CardContent className="flex flex-col items-center gap-4 py-12">
          <Check className="h-12 w-12 text-accent" aria-hidden />
          <h2 className="text-2xl font-semibold font-heading">{t("active")}</h2>
          <Link
            href="/dashboard"
            className={cn(buttonVariants({ size: "lg" }), "mt-4")}
          >
            {t("continue")}
          </Link>
        </CardContent>
      </Card>
    );
  }

  // takingLonger
  return (
    <Card className="text-center">
      <CardContent className="flex flex-col items-center gap-4 py-12">
        <AlertCircle className="h-12 w-12 text-muted-foreground" aria-hidden />
        <h2 className="text-2xl font-semibold font-heading">
          {t("takingLonger.heading")}
        </h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          {t("takingLonger.body")}
        </p>
        <p className="mt-2 font-mono text-sm text-muted-foreground">
          {p.invoiceId}
        </p>
        <div className="mt-2 flex w-full max-w-xs flex-col gap-2">
          <button
            onClick={p.onRefresh}
            className={buttonVariants({ variant: "outline", size: "lg" })}
          >
            {t("refresh")}
          </button>
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
