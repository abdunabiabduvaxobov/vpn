/**
 * Locale → currency mapping + currency formatter (D-04).
 *
 * Owned by Plan 04-05; included in this worktree so that Plan 04-06's
 * /dashboard server page can import { currencyForLocale, type Locale }
 * to pick a currency for its fetchPlans() call. The orchestrator's merge
 * of the wave-3 worktrees will resolve to this version (Plan 04-05's
 * draft is intended to be identical — see 04-05 plan §Task 1).
 *
 * Mapping (D-04 default):
 *   ru → RUB
 *   en → USD
 *   es → EUR
 *
 * `?currency=` query param + `pricing_currency` cookie can override per
 * request; this module only knows about the mapping. The page reads the
 * override and passes it as the second arg.
 *
 * NOT server-only — client components (e.g. CurrencySwitcher) need the
 * Currency type as well.
 */

export type Currency = "USD" | "EUR" | "RUB";
export type Locale = "ru" | "en" | "es";

const LOCALE_DEFAULT: Record<Locale, Currency> = {
  ru: "RUB",
  en: "USD",
  es: "EUR",
};

export function currencyForLocale(locale: string, override?: string): Currency {
  const o = (override ?? "").toUpperCase();
  if (o === "USD" || o === "EUR" || o === "RUB") return o;
  return LOCALE_DEFAULT[locale as Locale] ?? "USD";
}

export function formatPrice(
  amount: number,
  currency: Currency,
  locale: Locale,
): string {
  const intlLocale =
    locale === "ru" ? "ru-RU" : locale === "es" ? "es-ES" : "en-US";
  try {
    return new Intl.NumberFormat(intlLocale, {
      style: "currency",
      currency,
      minimumFractionDigits: 0,
      maximumFractionDigits: 2,
    }).format(amount);
  } catch {
    // Fallback for an unrecognised currency code (shouldn't happen given
    // the union but defensive against e.g. an experimental ICU build).
    return `${amount.toFixed(2)} ${currency}`;
  }
}
