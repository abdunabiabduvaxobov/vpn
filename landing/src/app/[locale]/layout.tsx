import type { Metadata } from "next";
import { Inter, JetBrains_Mono, Space_Grotesk } from "next/font/google";
import { NextIntlClientProvider, hasLocale } from "next-intl";
import { getTranslations, setRequestLocale } from "next-intl/server";
import { notFound } from "next/navigation";
import { routing } from "@/i18n/routing";
import { Navbar } from "@/components/common/navbar";
import { Footer } from "@/components/sections/footer";
import { organizationSchema, softwareApplicationSchema } from "@/lib/seo";
import "../globals.css";

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
        <NextIntlClientProvider>
          <Navbar />
          <main>{children}</main>
          <Footer />
        </NextIntlClientProvider>

        <script
          type="application/ld+json"
          // Plain string, server-rendered. Next.js statically prerenders the
          // page so the JSON ends up in the HTML and is crawlable.
          dangerouslySetInnerHTML={{ __html: JSON.stringify(ldOrganization) }}
        />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(ldSoftwareApp) }}
        />
      </body>
    </html>
  );
}
