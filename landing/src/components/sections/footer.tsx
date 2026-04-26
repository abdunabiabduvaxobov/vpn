import { useTranslations } from "next-intl";
import {
  TelegramIcon,
  XIcon,
  YouTubeIcon,
  GitHubIcon,
} from "@/components/common/brand-icons";
import { Logo } from "@/components/common/logo";
import { LocaleSwitcher } from "@/components/common/locale-switcher";
import { buttonVariants } from "@/components/ui/button";
import { APP_DOWNLOAD, SOCIAL_LINKS } from "@/lib/constants";
import { Link } from "@/i18n/navigation";

const PRODUCT_LINKS = [
  { key: "features", href: "#features" },
  { key: "servers", href: "#" },
  { key: "pricing", href: "#" },
  { key: "download", href: APP_DOWNLOAD.android, external: true },
] as const;

const COMPANY_LINKS = [
  { key: "about", href: "#" },
  { key: "blog", href: "#" },
  { key: "careers", href: "#" },
  { key: "press", href: "#" },
] as const;

const SUPPORT_LINKS = [
  { key: "help", href: "#" },
  { key: "contact", href: SOCIAL_LINKS.telegram, external: true },
  { key: "status", href: "#" },
  { key: "faq", href: "#faq" },
] as const;

const LEGAL_LINKS = [
  { key: "privacy", href: "/privacy", localised: true },
  { key: "terms", href: "#" },
  { key: "refund", href: "#" },
] as const;

const SOCIAL_ICONS = [
  { href: SOCIAL_LINKS.telegram, label: "Telegram", Icon: TelegramIcon },
  { href: SOCIAL_LINKS.twitter, label: "X (Twitter)", Icon: XIcon },
  { href: SOCIAL_LINKS.youtube, label: "YouTube", Icon: YouTubeIcon },
  { href: SOCIAL_LINKS.github, label: "GitHub", Icon: GitHubIcon },
] as const;

/**
 * Footer skeleton — full link wiring lives here from Phase 2 onward, but
 * Phase 7 will polish styling, hover glows, and add the locale dropdown
 * variant. Server component (no client state).
 */
export function Footer() {
  const t = useTranslations("footer");

  // Helper that resolves either an anchor (#features), an absolute URL, or a
  // localised in-app path through next-intl's <Link>.
  const renderLink = (
    item: { key: string; href: string; external?: boolean; localised?: boolean },
    namespace: string,
  ) => {
    const label = t(`columns.${namespace}.links.${item.key}`);
    const className =
      "text-sm text-muted-foreground transition hover:text-foreground";

    if (item.localised) {
      return (
        <Link href={item.href} className={className}>
          {label}
        </Link>
      );
    }
    return (
      <a
        href={item.href}
        className={className}
        {...(item.external
          ? { target: "_blank", rel: "noopener noreferrer" }
          : {})}
      >
        {label}
      </a>
    );
  };

  return (
    <footer className="border-t border-border-subtle bg-background">
      <div className="mx-auto max-w-7xl px-4 py-16 md:px-6 lg:px-8">
        <div className="grid grid-cols-2 gap-10 md:grid-cols-3 lg:grid-cols-5">
          {/* Brand column — spans two on lg, one on md */}
          <div className="col-span-2 lg:col-span-2">
            <Logo />
            <p className="mt-4 max-w-xs text-sm text-muted-foreground">
              {t("tagline")}
            </p>
            <a
              href={APP_DOWNLOAD.android}
              className={buttonVariants() + " mt-6"}
            >
              {t("downloadCta")}
            </a>
          </div>

          {[
            ["product", PRODUCT_LINKS],
            ["company", COMPANY_LINKS],
            ["support", SUPPORT_LINKS],
            ["legal", LEGAL_LINKS],
          ].map(([ns, items]) => (
            <div key={ns as string}>
              <h3 className="font-heading text-sm font-semibold uppercase tracking-wider text-foreground">
                {t(`columns.${ns as string}.title`)}
              </h3>
              <ul className="mt-4 space-y-3">
                {(items as readonly { key: string; href: string }[]).map(
                  (item) => (
                    <li key={item.key}>{renderLink(item, ns as string)}</li>
                  ),
                )}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-16 flex flex-col items-start justify-between gap-6 border-t border-border-subtle pt-8 md:flex-row md:items-center">
          <p className="text-xs text-subtle-foreground">{t("copyright")}</p>

          <div className="flex items-center gap-3">
            {SOCIAL_ICONS.filter(({ href }) => href !== "#").map(
              ({ href, label, Icon }) => (
                <a
                  key={label}
                  href={href}
                  aria-label={label}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="rounded-full border border-border-subtle bg-surface/40 p-2 text-muted-foreground transition hover:border-primary hover:text-primary"
                >
                  <Icon className="h-4 w-4" />
                </a>
              ),
            )}
            <LocaleSwitcher className="ml-2" />
          </div>
        </div>
      </div>
    </footer>
  );
}
