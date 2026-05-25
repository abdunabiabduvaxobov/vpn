import { redirect } from "@/i18n/navigation";
import { getSession } from "@/lib/session";

import { PollClient } from "./poll-client";

/**
 * WR-03 closure: validate the shape of `invoiceId` BEFORE forwarding it to
 * the upstream proxy / backend. The backend already performs an ownership
 * check + UUID parse (404 on miss), but a malformed or absurdly long ID
 * lands in the URL we construct for `/api/v1/invoices/<id>`. Defensive
 * landing-side validation keeps the upstream URL bounded and rejects
 * obvious junk with a redirect rather than a wasted backend call.
 *
 * Accepts: 1-64 chars of [A-Za-z0-9_-]. This is a superset of the
 * canonical UUID format used by lava.top while remaining narrow enough
 * to reject path-traversal probes and overflow-sized inputs.
 */
const RAW_INVOICE_ID_RE = /^[A-Za-z0-9_-]{1,64}$/;

/**
 * /<locale>/pay/success — auth-gated server page that hosts <PollClient/>.
 *
 * Pre-conditions enforced server-side BEFORE any client JS ships:
 *   1. Session must be authenticated. If not → /login?next=/pay/success?invoiceId=X.
 *   2. ?invoiceId must be present. If not → /dashboard (the user is signed in
 *      but reached this URL without an invoice context; nothing useful to do).
 *
 * Inherits dynamic = "force-dynamic" from the (app) layout (Plan 02) so the
 * cookie/session read happens at request time. runtime = "nodejs" is explicit
 * here to mirror Plan 03's proxy / Plan 04's callback — the page reads server-
 * only modules (session.ts has `import "server-only"`).
 *
 * The page intentionally renders nothing below <PollClient/> — UI-SPEC pins
 * /pay/success to the single status card with no extra navigation chrome
 * beyond the (app) layout's NavbarApp + Footer.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type Props = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ invoiceId?: string }>;
};

export default async function PaySuccessPage({ params, searchParams }: Props) {
  const { locale } = await params;
  const sp = await searchParams;

  const session = await getSession();
  if (!session.isAuthed) {
    // Preserve the invoiceId in the next= round-trip so the user lands back
    // on this exact poll page after sign-in.
    const next = `/pay/success${
      sp.invoiceId ? `?invoiceId=${encodeURIComponent(sp.invoiceId)}` : ""
    }`;
    return redirect({
      href: { pathname: "/login", query: { next } },
      locale,
    });
  }

  if (!sp.invoiceId) {
    // No invoice context — send the user to a useful default surface.
    return redirect({ href: "/dashboard", locale });
  }

  // WR-03: shape-validate invoiceId before propagating it to the proxy /
  // backend. A junk value here would otherwise build an absurdly-long
  // upstream URL for `/api/v1/invoices/<id>`. Reject by redirecting to
  // /pay/fail with a generic reason — same UX as an unknown-invoice 404.
  if (!RAW_INVOICE_ID_RE.test(sp.invoiceId)) {
    return redirect({
      href: { pathname: "/pay/fail", query: { reason: "default" } },
      locale,
    });
  }

  return (
    <main className="mx-auto max-w-md px-6 py-16">
      <PollClient invoiceId={sp.invoiceId} locale={locale} />
    </main>
  );
}
