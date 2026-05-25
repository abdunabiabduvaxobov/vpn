import type { Metadata } from "next";
import { Inter, JetBrains_Mono, Space_Grotesk } from "next/font/google";
import { NextIntlClientProvider, hasLocale } from "next-intl";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";
import { routing } from "@/i18n/routing";
import { organizationSchema, softwareApplicationSchema } from "@/lib/seo";
import "../globals.css";

/**
 * WR-04 closure: serialize an object as JSON safe to drop into a
 * `<script type="application/ld+json">` body via `dangerouslySetInnerHTML`.
 *
 * Plain `JSON.stringify` does NOT escape:
 *   - `</script>` (or `</Script>`) — would close the surrounding script tag
 *   - `<!--`      — would start an HTML comment context
 *   - `<![CDATA[` — XHTML / CDATA injection (XHTML legacy concern)
 *   - U+2028 / U+2029 — line separators that legacy JS parsers treat as
 *     newlines in JSON-in-script (the JSON-LD MIME spec doesn't strictly
 *     need this but it's the canonical hardened-JSON serializer pattern)
 *
 * Replacing the FIRST `<` of each tag-like sequence with `<` keeps the
 * resulting JSON valid (the parsed string still contains the literal `<`)
 * while making it unparseable as an HTML opening sequence.
 */
function safeJsonLd(obj: unknown): string {
  // Use RegExp constructor with explicit unicode escapes so the source file
  // contains no literal U+2028 / U+2029 (which the TypeScript regex literal
  // parser rejects as unterminated regex).
  return JSON.stringify(obj)
    .replace(/</g, "\\u003c")
    .replace(new RegExp("\u2028", "g"), "\\u2028")
    .replace(new RegExp("\u2029", "g"), "\\u2029");
}

const inter = Inter({
  variable: "--font-inter",
  subsets: ["cyrillic", "latin"],
  display: "swap",
});

const spaceGrotesk = Space_Grotesk({
  variable: "--font-space-grotesk",
  subsets: ["latin"],
  display: "swap",
});

const jetbrainsMono = JetBrains_Mono({
  variable: "--font-jetbrains-mono",
  subsets: ["cyrillic", "latin"],
  display: "swap",
});

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

  const t = await getTranslations({ locale, namespace: "metadata" });
  const siteUrl = "https://vpn.mydayai.uz";

  return {
    metadataBase: new URL(siteUrl),
    title: { default: t("title"), template: "%s | Rise VPN" },
    description: t("description"),
    keywords: t("keywords"),
    authors: [{ name: "Rise VPN" }],
    alternates: {
      canonical: `/${locale}/`,
      languages: {
        ru: "/ru/",
        en: "/en/",
        es: "/es/",
        "x-default": "/ru/",
      },
    },
    openGraph: {
      type: "website",
      locale: locale === "ru" ? "ru_RU" : locale === "es" ? "es_ES" : "en_US",
      url: `${siteUrl}/${locale}/`,
      siteName: "Rise VPN",
      title: t("ogTitle"),
      description: t("ogDescription"),
      images: [
        {
          url: "/og-image.png",
          width: 1200,
          height: 630,
          alt: "Rise VPN",
        },
      ],
    },
    twitter: {
      card: "summary_large_image",
      title: t("ogTitle"),
      description: t("ogDescription"),
      images: ["/og-image.png"],
    },
    robots: {
      index: true,
      follow: true,
      googleBot: {
        index: true,
        follow: true,
        "max-image-preview": "large",
        "max-snippet": -1,
        "max-video-preview": -1,
      },
    },
  };
}

export default async function LocaleLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  if (!hasLocale(routing.locales, locale)) notFound();

  // Required for static rendering with next-intl on App Router.
  setRequestLocale(locale);

  // SEO structured data — Organization is brand-wide; SoftwareApplication
  // describes the actual VPN app. FAQPage schema is appended in the FAQ
  // section component (Phase 6) so it stays colocated with the visible Q&A.
  const t = await getTranslations({ locale, namespace: "metadata" });
  const ldOrganization = organizationSchema(locale);
  const ldSoftwareApp = softwareApplicationSchema(locale, t("description"));

  return (
    <html
      lang={locale}
      className={`${inter.variable} ${spaceGrotesk.variable} ${jetbrainsMono.variable}`}
    >
      <body
        id="top"
        className="min-h-screen bg-background text-foreground antialiased"
      >
        <NextIntlClientProvider>{children}</NextIntlClientProvider>

        <script
          type="application/ld+json"
          // Plain string, server-rendered. Next.js statically prerenders the
          // page so the JSON ends up in the HTML and is crawlable.
          // WR-04: safeJsonLd escapes `<` so an embedded `</script>` cannot
          // break out of the script context (also closes the U+2028 /
          // U+2029 line-separator issue and any future `<!--` injection).
          dangerouslySetInnerHTML={{ __html: safeJsonLd(ldOrganization) }}
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: safeJsonLd(ldSoftwareApp) }}
        />
      </body>
    </html>
  );
}
