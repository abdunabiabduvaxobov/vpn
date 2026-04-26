import { routing } from "@/i18n/routing";

/**
 * Root "/" page. Pure static export = no middleware = no automatic locale
 * redirect at request time. nginx handles the redirect in production
 * (`location = / { return 302 /ru/; }`); this component is the in-build
 * fallback so the page works when opened directly from the file system or
 * from a host without the nginx rule.
 */
export default function RootPage() {
  const target = `/${routing.defaultLocale}/`;
  return (
    <html lang={routing.defaultLocale}>
      <head>
        <meta httpEquiv="refresh" content={`0; url=${target}`} />
        <link rel="canonical" href={target} />
        <title>Rise VPN</title>
      </head>
      <body>
        <p>
          Redirecting to <a href={target}>{target}</a>…
        </p>
      </body>
    </html>
  );
}
