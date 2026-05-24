/**
 * Phase 4 D-04 — locale → default currency mapping + i18n-aware price formatter.
 *
 * Intentionally NOT `import "server-only"` so this module can be imported by
 * both server components (Pricing page) and client components (CurrencySwitcher
 * chip group needs the Currency type + allow-list).
 *
 * D-04 contract:
 *   ru → RUB   en → USD   es → EUR
 * `?currency=` query param (allow-listed via `currencyForLocale`) overrides
 * the locale default. Any value outside USD/EUR/RUB falls back to the locale
 * default (T-04-05-05 closure — sanitises malicious `?currency=`).
 */

export type Currency = "USD" | "EUR" | "RUB";
export type Locale = "ru" | "en" | "es";

const LOCALE_DEFAULT: Record<Locale, Currency> = {
  ru: "RUB",
  en: "USD",
  es: "EUR",
};

/**
 * Resolve the effective currency for a request.
 *
 *   currencyForLocale("ru")          → "RUB"     (default)
 *   currencyForLocale("ru", "USD")   → "USD"     (override)
 *   currencyForLocale("ru", "usd")   → "USD"     (case-insensitive)
 *   currencyForLocale("ru", "XYZ")   → "RUB"     (invalid → locale default)
 *   currencyForLocale("xx")          → "USD"     (unknown locale → USD)
 */
export function currencyForLocale(locale: string, override?: string): Currency {
  const o = (override ?? "").toUpperCase();
  if (o === "USD" || o === "EUR" || o === "RUB") return o;
  return LOCALE_DEFAULT[locale as Locale] ?? "USD";
}

/**
 * Format a numeric amount as a localized currency string.
 *
 * Uses Intl.NumberFormat with the BCP-47 locale that matches the URL locale:
 *   ru → ru-RU      en → en-US      es → es-ES
 *
 * `minimumFractionDigits: 0` so whole-number prices ($5) don't render as $5.00,
 * `maximumFractionDigits: 2` so $4.99 keeps its cents. Falls back to a plain
 * `${amount} ${currency}` string if the runtime rejects the currency code.
 */
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
    // Fallback for unrecognised currency or Intl runtime failure.
    return `${amount.toFixed(2)} ${currency}`;
  }
}
