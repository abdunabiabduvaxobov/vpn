"use client";

import { useTranslations } from "next-intl";
import { motion, useReducedMotion } from "motion/react";
import {
  EyeOff,
  Globe,
  Shield,
  Smartphone,
  Unlock,
  Zap,
  type LucideIcon,
} from "lucide-react";
import { fadeInUp, inViewOnce, staggerContainer } from "@/lib/animations";

// Map message-bound icon names → real Lucide components. Keeps the message
// JSON purely declarative (no JSX) and lets translators add/remove items
// without touching code, as long as the icon name is in this whitelist.
const ICONS: Record<string, LucideIcon> = {
  Zap,
  Shield,
  EyeOff,
  Globe,
  Unlock,
  Smartphone,
};

type Item = { icon: keyof typeof ICONS | string; title: string; description: string };

export function Features() {
  const t = useTranslations("features");
  const reduceMotion = useReducedMotion();

  // next-intl v4 returns typed nested objects with `t.raw()`. Items are
  // declared as a JSON array in messages so we read them as a plain array.
  const items = (t.raw("items") ?? []) as Item[];

  return (
    <section
      id="features"
      className="relative overflow-hidden border-t border-border-subtle bg-background py-24 md:py-32"
    >
      <div className="mx-auto max-w-7xl px-4 md:px-6 lg:px-8">
        <motion.div
          variants={fadeInUp}
          initial={reduceMotion ? "visible" : "hidden"}
          whileInView="visible"
          viewport={inViewOnce}
          className="mx-auto max-w-2xl text-center"
        >
          <p className="font-mono text-xs uppercase tracking-[0.2em] text-primary">
            {t("eyebrow")}
          </p>
          <h2 className="mt-4 font-heading text-3xl font-semibold tracking-tight md:text-5xl">
            {t("title")}
          </h2>
          <p className="mt-4 text-base text-muted-foreground md:text-lg">
            {t("subtitle")}
          </p>
        </motion.div>

        <motion.ul
          variants={staggerContainer}
          initial={reduceMotion ? "visible" : "hidden"}
          whileInView="visible"
          viewport={inViewOnce}
          className="mt-16 grid grid-cols-1 gap-5 md:grid-cols-2 lg:grid-cols-3"
        >
          {items.map((item) => {
            const Icon = ICONS[item.icon] ?? Shield;
            return (
              <motion.li
                key={item.title}
                variants={fadeInUp}
                className="group relative rounded-xl border border-border-subtle bg-surface/40 p-6 backdrop-blur-xl transition-all duration-300 hover:-translate-y-1 hover:border-primary/40 hover:bg-surface/60 hover:shadow-[0_0_40px_-12px_hsl(var(--primary)/0.5)]"
              >
                {/* Icon in gradient circle. Inner ring + glow on hover. */}
                <div
                  className="mb-5 inline-flex h-11 w-11 items-center justify-center rounded-xl border border-border-subtle bg-gradient-to-br from-primary/20 to-accent/20 text-primary transition-colors group-hover:border-primary/50 group-hover:text-primary-glow"
                  aria-hidden="true"
                >
                  <Icon className="h-5 w-5" />
                </div>

                <h3 className="font-heading text-lg font-semibold tracking-tight text-foreground">
                  {item.title}
                </h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
                  {item.description}
                </p>
              </motion.li>
            );
          })}
        </motion.ul>
      </div>
    </section>
  );
}
