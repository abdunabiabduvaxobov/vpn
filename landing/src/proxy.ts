import createMiddleware from "next-intl/middleware";
import { routing } from "./i18n/routing";

export default createMiddleware(routing);

export const config = {
  // Exclude /api and /auth from locale-prefix middleware. /auth/callback
  // is the locale-LESS OAuth receiver (Plan 04-04 D-10) — without this
  // exclusion the middleware 302's it to /ru/auth/callback which has no
  // matching route, and Apple/Google form_posts 404. (Phase 4 Plan 04-08
  // Rule 1 fix, surfaced by Playwright CSRF smoke.)
  matcher: "/((?!api|auth|_next|_vercel|.*\\..*).*)",
};
