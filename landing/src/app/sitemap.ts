import type { MetadataRoute } from "next";
import { routing } from "@/i18n/routing";
import { SITE } from "@/lib/constants";

export const dynamic = "force-static";

// Build the per-page alternates block once. Search engines use it to
// understand that /ru, /en and /uz are the same page rendered in different
// languages, not three competing duplicates.
function alternatesFor(path: string): Record<string, string> {
  const langs: Record<string, string> = Object.fromEntries(
    routing.locales.map((l) => [l, `${SITE.url}/${l}${path}`]),
  );
  langs["x-default"] = `${SITE.url}/${routing.defaultLocale}${path}`;
  return langs;
}

export default function sitemap(): MetadataRoute.Sitemap {
  const now = new Date();
  const entries: MetadataRoute.Sitemap = [];

  // Home: one entry per locale, with hreflang alternates between them.
  for (const locale of routing.locales) {
    entries.push({
      url: `${SITE.url}/${locale}/`,
      lastModified: now,
      changeFrequency: "weekly",
      priority: locale === routing.defaultLocale ? 1 : 0.8,
      alternates: { languages: alternatesFor("/") },
    });
  }

  // Privacy policy. Lower priority than the marketing home but still
  // indexable — Google needs to be able to fetch it for app store reviews.
  for (const locale of routing.locales) {
    entries.push({
      url: `${SITE.url}/${locale}/privacy/`,
      lastModified: now,
      changeFrequency: "yearly",
      priority: 0.5,
      alternates: { languages: alternatesFor("/privacy/") },
    });
  }

  return entries;
}
