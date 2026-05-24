import { NavbarApp } from "@/components/common/navbar-app";
import { Footer } from "@/components/sections/footer";
import { Toaster } from "@/components/ui/toast";

/**
 * App route-group layout (Phase 4 D-18 — server-side cookie detection).
 *
 * `dynamic = 'force-dynamic'` forces every page under (app)/ to opt OUT of
 * static prerendering. Required because `<NavbarApp>` reads the `rv_at` /
 * `rv_user` HttpOnly cookies via `getSession()` to render different links
 * for logged-in vs logged-out users. Without `force-dynamic`, Next.js would
 * try to prerender each page at build time and the cookie read would either
 * crash (Next 16 disallows cookies() in static contexts) or capture the
 * empty-cookie state forever.
 *
 * Pages in this group: /login (Plan 04), /dashboard (Plan 06),
 * /pricing (Plan 05), /pay/success + /pay/fail (Plan 07).
 *
 * Toaster is mounted once at this layout level so any client component
 * inside the (app) tree can call `toast({title, description})` from
 * @/components/ui/toast.
 */
export const dynamic = "force-dynamic";

export default function AppLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <>
      <NavbarApp />
      <main>{children}</main>
      <Footer />
      <Toaster />
    </>
  );
}
