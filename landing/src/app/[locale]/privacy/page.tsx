import type { Metadata } from "next";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { hasLocale } from "next-intl";
import { notFound } from "next/navigation";
import { routing } from "@/i18n/routing";
import { SITE } from "@/lib/constants";

export function generateStaticParams() {
  return routing.locales.map((locale) => ({ locale }));
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  if (!hasLocale(routing.locales, locale)) return {};

  const t = await getTranslations({ locale, namespace: "privacy" });
  return {
    title: t("title"),
    description: t("metaDescription"),
    alternates: {
      canonical: `/${locale}/privacy/`,
      languages: Object.fromEntries(
        routing.locales.map((l) => [l, `/${l}/privacy/`]),
      ),
    },
    openGraph: {
      type: "article",
      url: `${SITE.url}/${locale}/privacy/`,
      title: t("title"),
      description: t("metaDescription"),
    },
    robots: { index: true, follow: true },
  };
}

type Section = { id: string; title: string; body: string };

export default async function PrivacyPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  if (!hasLocale(routing.locales, locale)) notFound();
  setRequestLocale(locale);

  const t = await getTranslations("privacy");
  const sections = (t.raw("sections") ?? []) as Section[];

  return (
    <article className="mx-auto max-w-3xl px-4 py-20 md:px-6 md:py-28 lg:px-8">
      <header className="border-b border-border-subtle pb-10">
        <p className="font-mono text-xs uppercase tracking-[0.2em] text-primary">
          {t("lastUpdated")}
        </p>
        <h1 className="mt-3 font-heading text-4xl font-semibold tracking-tight md:text-5xl">
          {t("title")}
        </h1>
        <p className="mt-6 text-base leading-relaxed text-muted-foreground md:text-lg">
          {t("intro")}
        </p>
      </header>

      {/* Sticky table-of-contents only on lg — keeps the prose column itself
          single, narrow and very readable. The TOC is a nice-to-have, not the
          load-bearing nav, so we hide it on smaller viewports. */}
      <nav
        aria-label="Sections"
        className="mt-10 grid gap-2 rounded-xl border border-border-subtle bg-surface/40 p-5 text-sm backdrop-blur md:grid-cols-2"
      >
        {sections.map((s) => (
          <a
            key={s.id}
            href={`#${s.id}`}
            className="text-muted-foreground transition hover:text-primary"
          >
            {s.title}
          </a>
        ))}
      </nav>

      <div className="mt-12 space-y-12">
        {sections.map((s) => (
          <section
            key={s.id}
            id={s.id}
            className="scroll-mt-24"
          >
            <h2 className="font-heading text-xl font-semibold tracking-tight text-foreground md:text-2xl">
              {s.title}
            </h2>
            {/* Body uses \n\n for paragraph breaks and \n for soft line breaks
                inside a paragraph (used for bullet lists). whitespace-pre-line
                preserves the linebreaks without an MDX/markdown step. */}
            <div className="mt-4 whitespace-pre-line text-base leading-relaxed text-muted-foreground">
              {s.body}
            </div>
          </section>
        ))}
      </div>

      <footer className="mt-20 border-t border-border-subtle pt-8 text-sm text-subtle-foreground">
        {t("lastUpdated")}
      </footer>
    </article>
  );
}
