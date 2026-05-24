"use client";

import { useTransition } from "react";

import { usePathname, useRouter } from "@/i18n/navigation";
import { cn } from "@/lib/utils";
import type { Currency, Locale } from "@/lib/locale-currency";

/**
 * CurrencySwitcher — three-chip group (USD / EUR / RUB) used in the /pricing
 * hero. Owns two pieces of state:
 *
 *   1. The visible URL — `?currency=<C>` is pushed via next-intl's locale-aware
 *      router.replace so the server component re-renders with the new currency
 *      on the next paint.
 *   2. A 30-day `pricing_currency` cookie — read on subsequent visits as a
 *      stickier default than the per-request `?currency=`. Cookie is NOT
 *      HttpOnly (the chip group needs to read its own previous choice; it
 *      contains no sensitive data — just "USD"/"EUR"/"RUB"). SameSite=Lax so
 *      it survives a top-level redirect from /login → /pricing. Secure flag
 *      mirrors `location.protocol === "https:"` so http://localhost still
 *      stores the cookie in dev.
 *
 * The component does NOT compute the active currency itself — that decision
 * lives in `currencyForLocale()` on the server. We just reflect the active
 * prop visually and emit the user's choice as a query param.
 */

const CHOICES: Currency[] = ["USD", "EUR", "RUB"];

type Props = { locale: Locale; active: Currency };

export function CurrencySwitcher({ active }: Props) {
  const router = useRouter();
  const pathname = usePathname();
  const [pending, start] = useTransition();

  return (
    <div
      role="group"
      aria-label="Currency"
      className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-surface/40 p-1 backdrop-blur"
    >
      {CHOICES.map((c) => {
        const isActive = c === active;
        return (
          <button
            key={c}
            type="button"
            disabled={pending || isActive}
            onClick={() => {
              // Persist for next visit. Path=/ so other (app) pages see it.
              const secure =
                typeof location !== "undefined" &&
                location.protocol === "https:"
                  ? "; Secure"
                  : "";
              document.cookie = `pricing_currency=${c}; Max-Age=2592000; Path=/; SameSite=Lax${secure}`;
              start(() => {
                // Build a new URL preserving any other query params
                // (e.g., ?plan=pro&period=monthly from a returning checkout).
                const search = new URLSearchParams(
                  typeof window !== "undefined" ? window.location.search : "",
                );
                search.set("currency", c);
                router.replace(`${pathname}?${search.toString()}`);
              });
            }}
            className={cn(
              "rounded-full px-3 py-1 font-mono text-xs uppercase tracking-wider transition",
              isActive
                ? "bg-accent text-accent-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
            aria-current={isActive ? "true" : undefined}
          >
            {c}
          </button>
        );
      })}
    </div>
  );
}
