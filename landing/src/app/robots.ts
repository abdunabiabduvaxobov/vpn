import type { MetadataRoute } from "next";
import { SITE } from "@/lib/constants";

// Required when output: 'export' — flags this route as fully static so it
// emits at build time instead of being treated as dynamic.
export const dynamic = "force-static";

export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      {
        userAgent: "*",
        allow: "/",
        disallow: ["/api/", "/_next/"],
      },
    ],
    sitemap: `${SITE.url}/sitemap.xml`,
  };
}
