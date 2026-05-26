"use client";

import { Popover } from "@base-ui/react/popover";
import { useTranslations } from "next-intl";

import { cn } from "@/lib/utils";

/**
 * UserMenu — logged-in navbar dropdown.
 *
 * Trigger: avatar disc showing the first letter of the user's email (or a
 * generic user glyph when the email is empty — happens when `rv_user` cookie
 * de-synced but `rv_at` is still present; see lib/session.ts).
 *
 * Popup: email line + `<form action="/api/auth/logout" method="POST">` Sign
 * out button. The form submits without JavaScript so logout still works for
 * users with JS disabled or while React hydrates. Plan 03's Node proxy will
 * own /api/auth/logout — it clears `rv_at` + `rv_user` cookies and redirects
 * back to /.
 */

interface UserMenuProps {
  email: string;
}

export function UserMenu({ email }: UserMenuProps) {
  const t = useTranslations("navbar.app");
  const initial = email ? email.charAt(0).toUpperCase() : "";

  return (
    <Popover.Root>
      <Popover.Trigger
        aria-label={email || "Account menu"}
        className={cn(
          "inline-flex h-8 w-8 items-center justify-center rounded-full border border-border-subtle bg-surface-elevated text-sm font-medium text-foreground transition hover:border-border focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none",
        )}
      >
        {initial || (
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="h-4 w-4"
            aria-hidden="true"
          >
            <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
            <circle cx="12" cy="7" r="4" />
          </svg>
        )}
      </Popover.Trigger>

      <Popover.Portal>
        <Popover.Positioner sideOffset={8} align="end">
          <Popover.Popup
            className={cn(
              "z-50 min-w-[14rem] rounded-[var(--radius-lg)] border border-border-subtle bg-popover p-2 text-sm text-popover-foreground shadow-lg outline-none transition-all data-[ending-style]:opacity-0 data-[starting-style]:translate-y-1 data-[starting-style]:opacity-0",
            )}
          >
            {email ? (
              <div className="border-b border-border-subtle px-3 py-2 text-xs text-muted-foreground">
                <div className="font-mono break-all">{email}</div>
              </div>
            ) : null}

            <form action="/api/auth/logout" method="POST" className="pt-1">
              <button
                type="submit"
                data-testid="sign-out-button"
                className="w-full rounded-md px-3 py-2 text-left text-sm text-foreground transition hover:bg-muted"
              >
                {t("signOut")}
              </button>
            </form>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
