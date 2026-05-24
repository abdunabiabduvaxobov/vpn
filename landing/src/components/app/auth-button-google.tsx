import Image from "next/image";
import { getTranslations } from "next-intl/server";

import { startOAuthForm } from "@/app/[locale]/(app)/login/start-oauth";

/**
 * Google "Sign in with Google" button.
 *
 * Server Component — same form-action pattern as AuthButtonApple.
 *
 * Styling per Google's Identity Branding Guidelines (the "Continue with
 * Google" pattern):
 * - White background (#fff)
 * - Dark-grey foreground (#3c4043)
 * - 1px #dadce0 border
 * - 4-colour G logomark at left
 * - 44px minimum tap target (h-11 = 44px in Tailwind 4)
 *
 * alt="" on the Image because the adjacent <span> text already provides
 * the accessible name.
 */
type Props = {
  locale: string;
  next?: string;
  plan?: string;
  period?: string;
  currency?: string;
};

export async function AuthButtonGoogle({
  locale,
  next,
  plan,
  period,
  currency,
}: Props) {
  const t = await getTranslations("login.signIn");
  return (
    <form action={startOAuthForm} className="w-full">
      <input type="hidden" name="provider" value="google" />
      <input type="hidden" name="locale" value={locale} />
      {next && <input type="hidden" name="next" value={next} />}
      {plan && <input type="hidden" name="plan" value={plan} />}
      {period && <input type="hidden" name="period" value={period} />}
      {currency && <input type="hidden" name="currency" value={currency} />}
      <button
        type="submit"
        className="flex h-11 w-full items-center justify-center gap-2 rounded-[var(--radius-md)] border border-[#dadce0] bg-white px-4 font-medium text-[#3c4043] transition hover:bg-[#f8f9fa] focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <Image
          src="/brand/google/google-g.svg"
          alt=""
          width={20}
          height={20}
          aria-hidden="true"
        />
        <span>{t("google")}</span>
      </button>
    </form>
  );
}
