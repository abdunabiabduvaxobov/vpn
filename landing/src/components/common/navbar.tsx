"use client";

import { useEffect, useState } from "react";
import { useTranslations } from "next-intl";
import { Menu } from "lucide-react";
import { Logo } from "./logo";
import { LocaleSwitcher } from "./locale-switcher";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { APP_DOWNLOAD } from "@/lib/constants";

const ANCHORS = [
  { href: "#features", key: "features" },
  { href: "#how-it-works", key: "howItWorks" },
  { href: "#faq", key: "faq" },
] as const;

export function Navbar() {
  const t = useTranslations("nav");
  const [scrolled, setScrolled] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header
      className={`sticky top-0 z-50 w-full border-b transition-colors duration-200 ${
        scrolled
          ? "border-border-subtle bg-background/70 backdrop-blur-xl"
          : "border-transparent bg-transparent"
      }`}
    >
      <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 md:px-6 lg:px-8">
        <a href="#top" aria-label="Rise VPN — home">
          <Logo />
        </a>

        <nav className="hidden items-center gap-8 md:flex" aria-label="Primary">
          {ANCHORS.map((a) => (
            <a
              key={a.key}
              href={a.href}
              className="text-sm text-muted-foreground transition hover:text-foreground"
            >
              {t(a.key)}
            </a>
          ))}
        </nav>

        <div className="flex items-center gap-2 md:gap-3">
          <LocaleSwitcher className="hidden sm:inline-flex" />
          <a
            href={APP_DOWNLOAD.android}
            className={buttonVariants({ size: "sm" }) + " hidden sm:inline-flex"}
          >
            {t("download")}
          </a>

          <Sheet open={menuOpen} onOpenChange={setMenuOpen}>
            <SheetTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon"
                  className="md:hidden"
                  aria-label={t("menu")}
                >
                  <Menu className="h-5 w-5" />
                </Button>
              }
            />

            <SheetContent side="right" className="bg-background">
              <SheetHeader>
                <SheetTitle className="text-left">
                  <Logo />
                </SheetTitle>
              </SheetHeader>
              <nav
                className="mt-8 flex flex-col gap-4 px-4"
                aria-label="Mobile primary"
              >
                {ANCHORS.map((a) => (
                  <a
                    key={a.key}
                    href={a.href}
                    onClick={() => setMenuOpen(false)}
                    className="text-base text-foreground/90 transition hover:text-primary"
                  >
                    {t(a.key)}
                  </a>
                ))}
                <div className="mt-4">
                  <LocaleSwitcher />
                </div>
                <a
                  href={APP_DOWNLOAD.android}
                  onClick={() => setMenuOpen(false)}
                  className={buttonVariants() + " mt-4"}
                >
                  {t("download")}
                </a>
              </nav>
            </SheetContent>
          </Sheet>
        </div>
      </div>
    </header>
  );
}
