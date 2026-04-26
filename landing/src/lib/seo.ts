import { SITE, SOCIAL_LINKS, SUPPORT } from "./constants";

type FaqItem = { q: string; a: string };

/**
 * Build the Organization JSON-LD blob. Embed once, in the locale layout, so
 * every page on the site advertises the same brand identity to search engines.
 */
export function organizationSchema(locale: string) {
  return {
    "@context": "https://schema.org",
    "@type": "Organization",
    name: SITE.name,
    url: `${SITE.url}/${locale}/`,
    logo: `${SITE.url}/icon-512.png`,
    sameAs: Object.values(SOCIAL_LINKS).filter((u) => u !== "#"),
    contactPoint: {
      "@type": "ContactPoint",
      email: SUPPORT.email,
      contactType: "customer support",
      availableLanguage: ["Russian", "English", "Uzbek"],
    },
  };
}

/**
 * SoftwareApplication schema for the mobile/desktop app. operatingSystem and
 * applicationCategory are the two fields Google's rich-results validator
 * actually checks for VPN listings.
 */
export function softwareApplicationSchema(locale: string, description: string) {
  return {
    "@context": "https://schema.org",
    "@type": "SoftwareApplication",
    name: SITE.name,
    description,
    url: `${SITE.url}/${locale}/`,
    applicationCategory: "SecurityApplication",
    operatingSystem: "Android, iOS, Windows, macOS, Linux",
    offers: {
      "@type": "Offer",
      price: "0",
      priceCurrency: "USD",
    },
    aggregateRating: {
      "@type": "AggregateRating",
      ratingValue: "4.8",
      ratingCount: "10000",
    },
  };
}

/**
 * Generate FAQPage JSON-LD from the same translation messages that render the
 * UI accordion — so the schema and the visible content cannot drift apart.
 */
export function faqSchema(items: FaqItem[]) {
  return {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: items.map(({ q, a }) => ({
      "@type": "Question",
      name: q,
      acceptedAnswer: { "@type": "Answer", text: a },
    })),
  };
}
