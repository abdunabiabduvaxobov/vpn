import { getTranslations } from "next-intl/server";

import { AuthButtonApple } from "@/components/app/auth-button-apple";
import { AuthButtonGoogle } from "@/components/app/auth-button-google";
import { Logo } from "@/components/common/logo";
import { Link } from "@/i18n/navigation";

/**
 * /<locale>/login — sign-in entry point.
 *
 * UI per 04-UI-SPEC.md §"/login":
 *   - Center column, max-w-md, min-h-[80vh]
 *   - Logo at top
 *   - H1 + 1-line subhead
 *   - Apple button (full width)
 *   - 8px gap
 *   - Google button (full width)
 *   - 16px gap
 *   - "Back to home" subtle link
 *
 * The page is wrapped by the (app) layout (Plan 02) which mounts NavbarApp
 * + Footer + Toaster and pins `dynamic = 'force-dynamic'`. Redeclaring
 * `dynamic` here for clarity — the Server Action that submits the form
 * also implicitly requires a dynamic render.
 *
 * Error states (?error=oauth_state | ?error=oauth_denied) come from the
 * /auth/callback receiver (Task 3) and render as an inline alert below
 * the subhead. The error message uses i18n keys errors.oauthState /
 * errors.oauthDenied populated in Plan 01.
 *
 * Checkout-hint search params (next/plan/period/currency) flow straight
 * through the auth buttons into the state payload so Plan 07 can resume
 * the checkout intent after sign-in.
 */
export const dynamic = "force-dynamic";

type Props = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{
    next?: string;
    plan?: string;
    period?: string;
    currency?: string;
    error?: string;
  }>;
};

export default async function LoginPage({ params, searchParams }: Props) {
  const { locale } = await params;
  const sp = await searchParams;
  const t = await getTranslations("login");
  const tErr = await getTranslations("errors");

  return (
    <main className="mx-auto flex min-h-[80vh] max-w-md flex-col items-center justify-center px-6 py-12">
      <Logo className="mb-8" />
      <h1 className="font-heading text-3xl font-bold text-foreground lg:text-4xl">
        {t("heading")}
      </h1>
      <p className="mt-2 text-base text-muted-foreground">{t("subhead")}</p>

      {sp.error && (
        <div
          role="alert"
          className="mt-4 w-full rounded-[var(--radius-md)] border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
        >
          {sp.error === "oauth_state"
            ? tErr("oauthState")
            : tErr("oauthDenied")}
        </div>
      )}

      <div className="mt-8 flex w-full flex-col gap-2">
        <AuthButtonApple
          locale={locale}
          next={sp.next}
          plan={sp.plan}
          period={sp.period}
          currency={sp.currency}
        />
        <AuthButtonGoogle
          locale={locale}
          next={sp.next}
          plan={sp.plan}
          period={sp.period}
          currency={sp.currency}
        />
      </div>

      <Link
        href="/"
        className="mt-4 text-sm text-subtle-foreground hover:text-muted-foreground"
      >
        {t("backHome")}
      </Link>
    </main>
  );
}
