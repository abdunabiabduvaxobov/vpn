import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./src/i18n/request.ts");

const nextConfig: NextConfig = {
  // Node-runtime build (Phase 4, D-01). Standalone bundle is emitted to
  // .next/standalone for Docker/nginx deployment. nginx terminates TLS and
  // proxies /login, /dashboard, /pricing, /pay/*, /auth/*, /api/* to the Node
  // container; marketing pages stay statically pre-rendered.
  output: "standalone",
  trailingSlash: true,
};

export default withNextIntl(nextConfig);
