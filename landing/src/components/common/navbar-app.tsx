import { getTranslations } from "next-intl/server";

import { Link } from "@/i18n/navigation";
import { getSession } from "@/lib/session";
import { Logo } from "./logo";
import { LocaleSwitcher } from "./locale-switcher";
import { buttonVariants } from "@/components/ui/button";
import { UserMenu } from "@/components/app/user-menu";
import { cn } from "@/lib/utils";

/**
 * NavbarApp — server-component navbar for the (app) route group
 * (/login, /dashboard, /pricing, /pay/success, /pay/fail).
 *
 * Branches on `getSession()` (server-side cookie read of `rv_at`):
 *   - logged-out → shows "Pricing" + "Login" (link to /login)
 *   - logged-in  → shows "Pricing" + "Dashboard" + UserMenu (avatar + sign out)
 *
 * Implementing the branch SERVER-SIDE is the WEB-09 / SC #6 contract: no
 * flash of unauthenticated content while client JS hydrates, and zero
 * client-side cookie sniffing (cookies are HttpOnly and unreadable from JS
 * anyway). The (app) layout sets `dynamic = 'force-dynamic'` so this runs
 * on every request, not at build time.
 */
export async function NavbarApp() {
  const t = await getTranslations("navbar.app");
  const session = await getSession();

  return (
    <header className="sticky top-0 z-50 w-full border-b border-border-subtle bg-background/70 backdrop-blur-xl">
      <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 md:px-6 lg:px-8">
        <Link href="/" aria-label="Rise VPN — home">
          <Logo />
        </Link>

        <nav
          className="hidden items-center gap-8 md:flex"
          aria-label="Primary"
        >
          <Link
            href="/pricing"
            className="text-sm text-muted-foreground transition hover:text-foreground"
          >
            {t("pricing")}
          </Link>
          {session.isAuthed && (
            <Link
              href="/dashboard"
              className="text-sm text-muted-foreground transition hover:text-foreground"
            >
              {t("dashboard")}
            </Link>
          )}
        </nav>

        <div className="flex items-center gap-2 md:gap-3">
          <LocaleSwitcher className="hidden sm:inline-flex" />
          {session.isAuthed ? (
            <UserMenu email={session.email} />
          ) : (
            <Link
              href="/login"
              className={cn(buttonVariants({ size: "sm" }))}
            >
              {t("login")}
            </Link>
          )}
        </div>
      </div>
    </header>
  );
}
