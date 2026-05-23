"use client";

import { useTransition } from "react";
import { usePathname, useRouter } from "@/i18n/navigation";
import { routing, type Locale } from "@/i18n/routing";
import { useLocale } from "next-intl";

const LABELS: Record<Locale, string> = { ru: "RU", en: "EN", es: "ES" };

/**
 * Locale switcher rendered as three text buttons. Clicking re-pushes the
 * current pathname under the chosen locale via next-intl's localised router.
 * No dropdown — three locales fit cleanly inline and a list is more accessible
 * to keyboards and screen readers than a custom popover.
 */
export function LocaleSwitcher({ className }: { className?: string }) {
  const router = useRouter();
  const pathname = usePathname();
  const active = useLocale() as Locale;
  const [isPending, startTransition] = useTransition();

  return (
    <div
      className={`inline-flex items-center gap-1 rounded-full border border-border-subtle bg-surface/40 p-1 backdrop-blur ${className ?? ""}`}
      role="group"
      aria-label="Language"
    >
      {routing.locales.map((locale) => {
        const isActive = locale === active;
        return (
          <button
            key={locale}
            type="button"
            disabled={isPending || isActive}
            onClick={() => {
              startTransition(() => {
                router.replace(pathname, { locale });
              });
            }}
            className={`rounded-full px-3 py-1 font-mono text-xs uppercase tracking-wider transition ${
              isActive
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground"
            }`}
            aria-current={isActive ? "page" : undefined}
          >
            {LABELS[locale]}
          </button>
        );
      })}
    </div>
  );
}
