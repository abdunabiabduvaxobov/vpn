import {
  PaymentFailCard,
  type FailReason,
} from "@/components/app/payment-fail-card";

/**
 * /<locale>/pay/fail — Plan 04-07 (WEB-07).
 *
 * Server-rendered, dynamic. NOT auth-gated — a user whose session expired
 * mid-flow can still land here from a lava.top redirect and read why their
 * payment failed without being bounced to /login first.
 *
 * Untrusted query params (T-04-07-05 closure):
 *   - ?reason= — allow-listed to {default, declined, cancelled} via safeReason.
 *     Anything else falls back to "default" so a malicious value can't render
 *     attacker-controlled text or change the body branch.
 *   - ?plan=, ?period=, ?currency= — round-tripped INTO the Try-again /pricing
 *     URL but never rendered to the page. URLSearchParams URL-encodes them on
 *     write so any reflection happens inside an href attribute the user
 *     interacts with intentionally. /pricing then re-validates plan/period
 *     downstream against its known offers (it's already its own threat
 *     surface — see Plan 04-05).
 *
 * Inherits dynamic = "force-dynamic" from the (app) layout (Plan 02); force-
 * dynamic again here for clarity since the page reads searchParams every
 * request.
 */
export const dynamic = "force-dynamic";

function safeReason(r: string | undefined): FailReason {
  return r === "declined" || r === "cancelled" ? r : "default";
}

type SearchParams = {
  reason?: string;
  plan?: string;
  period?: string;
  currency?: string;
};

type Props = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<SearchParams>;
};

export default async function PayFailPage({ searchParams }: Props) {
  const sp = await searchParams;
  const reason = safeReason(sp.reason);

  // Build the Try-again target — preserve plan/period/currency when present
  // so the user lands back on /pricing with the same auto-checkout intent.
  // Fall back to bare /pricing when none were carried through.
  const qs = new URLSearchParams();
  if (sp.plan) qs.set("plan", sp.plan);
  if (sp.period) qs.set("period", sp.period);
  if (sp.currency) qs.set("currency", sp.currency);
  const tryAgainHref = qs.toString()
    ? `/pricing?${qs.toString()}`
    : "/pricing";

  return (
    <main className="mx-auto max-w-md px-6 py-16">
      <PaymentFailCard reason={reason} tryAgainHref={tryAgainHref} />
    </main>
  );
}
