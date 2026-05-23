import { Navbar } from "@/components/common/navbar";
import { Footer } from "@/components/sections/footer";

/**
 * Marketing route-group layout (Phase 4 D-06 / D-18).
 *
 * Wraps the static marketing surfaces (/, /privacy, opengraph-image) with
 * the existing client-side `<Navbar>` (anchor links + Download CTA) and
 * `<Footer>`. These pages stay statically prerendered — no cookie reads
 * happen here. The (app) sibling group owns the dynamic, authenticated
 * surfaces (/login /dashboard /pricing /pay/*) and renders `<NavbarApp>`
 * instead.
 *
 * Route groups (`(marketing)`) do not affect URL paths, so `/`, `/privacy`
 * etc. continue to resolve at their existing URLs.
 */
export default function MarketingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <>
      <Navbar />
      <main>{children}</main>
      <Footer />
    </>
  );
}
