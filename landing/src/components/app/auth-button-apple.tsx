import Image from "next/image";
import { getTranslations } from "next-intl/server";

import { startOAuthForm } from "@/app/[locale]/(app)/login/start-oauth";

/**
 * Apple "Sign in with Apple" button.
 *
 * Server Component — renders a <form action={startOAuthForm}> so the click
 * works without JavaScript while React hydrates (mirrors the UserMenu
 * sign-out form from Plan 02).
 *
 * Styling per Apple's "Sign in with Apple" Human Interface Guidelines:
 * - Black background (#000) + white foreground
 * - Brand mark at left, label centered
 * - 44px minimum tap target (h-11 = 44px in Tailwind 4)
 *
 * alt="" on the Image because the adjacent <span> text already provides
 * the accessible name — avoids "Apple Sign in with Apple" doubled
 * announcement.
 */
type Props = {
  locale: string;
  next?: string;
  plan?: string;
  period?: string;
  currency?: string;
};

export async function AuthButtonApple({
  locale,
  next,
  plan,
  period,
  currency,
}: Props) {
  const t = await getTranslations("login.signIn");
  return (
    <form action={startOAuthForm} className="w-full">
      <input type="hidden" name="provider" value="apple" />
      <input type="hidden" name="locale" value={locale} />
      {next && <input type="hidden" name="next" value={next} />}
      {plan && <input type="hidden" name="plan" value={plan} />}
      {period && <input type="hidden" name="period" value={period} />}
      {currency && <input type="hidden" name="currency" value={currency} />}
      <button
        type="submit"
        className="flex h-11 w-full items-center justify-center gap-2 rounded-[var(--radius-md)] bg-black px-4 font-medium text-white transition hover:opacity-90 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <Image
          src="/brand/apple/apple-sign-in.svg"
          alt=""
          width={20}
          height={20}
          aria-hidden="true"
        />
        <span>{t("apple")}</span>
      </button>
    </form>
  );
}
