import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./src/i18n/request.ts");

const nextConfig: NextConfig = {
  // Static export — nginx serves the pre-rendered tree from /opt/vpn/landing/dist.
  // No Node runtime on production. Middleware does NOT run on a static export, so
  // the "/" → "/ru" redirect is handled both by the root <meta refresh> page (so
  // dev preview and offline mirrors still work) and by nginx in production.
  output: "export",
  trailingSlash: true,
  images: {
    // Default image loader needs the Node runtime; static export must not use it.
    unoptimized: true,
  },
};

export default withNextIntl(nextConfig);
