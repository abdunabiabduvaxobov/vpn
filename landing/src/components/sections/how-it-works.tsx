"use client";

import { useTranslations } from "next-intl";
import { motion, useReducedMotion } from "motion/react";
import {
  Download,
  Server,
  ShieldCheck,
  type LucideIcon,
} from "lucide-react";
import { fadeInUp, inViewOnce, staggerContainer } from "@/lib/animations";

const ICONS: Record<string, LucideIcon> = { Download, Server, ShieldCheck };

type Step = {
  number: string;
  icon: keyof typeof ICONS | string;
  title: string;
  description: string;
};

export function HowItWorks() {
  const t = useTranslations("howItWorks");
  const reduceMotion = useReducedMotion();
  const steps = (t.raw("steps") ?? []) as Step[];

  const initial = reduceMotion ? "visible" : "hidden";
  // Skip the path-draw animation outright when reduced motion is requested.
  const drawTransition = reduceMotion
    ? { duration: 0 }
    : { duration: 1.6, ease: [0.16, 1, 0.3, 1] as const, delay: 0.2 };

  return (
    <section
      id="how-it-works"
      className="relative overflow-hidden border-t border-border-subtle bg-background py-24 md:py-32"
    >
      <div className="mx-auto max-w-6xl px-4 md:px-6 lg:px-8">
        <motion.div
          variants={fadeInUp}
          initial={initial}
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

        {/* The steps grid is positioned `relative` so the connector SVGs can
            sit absolutely behind the cards. The connector's intrinsic stroke
            length is animated from 0 → 1 via motion's `pathLength`. */}
        <motion.div
          initial={initial}
          whileInView="visible"
          viewport={inViewOnce}
          variants={staggerContainer}
          className="relative mt-20"
        >
          {/* Desktop horizontal connector — md+ only. Path runs through the
              centers of the three icon circles (y=44, x stops at 16% / 50% / 84%). */}
          <svg
            aria-hidden="true"
            viewBox="0 0 100 6"
            preserveAspectRatio="none"
            className="absolute left-0 right-0 top-[44px] z-0 hidden h-[6px] w-full md:block"
          >
            <defs>
              <linearGradient id="hiw-line-h" x1="0" y1="0" x2="1" y2="0">
                <stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity="0" />
                <stop offset="20%" stopColor="hsl(var(--primary))" />
                <stop offset="80%" stopColor="hsl(var(--accent))" />
                <stop offset="100%" stopColor="hsl(var(--accent))" stopOpacity="0" />
              </linearGradient>
            </defs>
            <motion.path
              d="M 0 3 L 100 3"
              stroke="url(#hiw-line-h)"
              strokeWidth="1"
              fill="none"
              vectorEffect="non-scaling-stroke"
              initial={{ pathLength: 0 }}
              whileInView={{ pathLength: 1 }}
              viewport={inViewOnce}
              transition={drawTransition}
              style={{ filter: "drop-shadow(0 0 6px hsl(var(--primary)/0.5))" }}
            />
          </svg>

          {/* Mobile vertical connector — runs down the left gutter at x=44px. */}
          <svg
            aria-hidden="true"
            viewBox="0 0 6 100"
            preserveAspectRatio="none"
            className="absolute left-[24px] top-0 z-0 block h-full w-[6px] md:hidden"
          >
            <defs>
              <linearGradient id="hiw-line-v" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity="0" />
                <stop offset="20%" stopColor="hsl(var(--primary))" />
                <stop offset="80%" stopColor="hsl(var(--accent))" />
                <stop offset="100%" stopColor="hsl(var(--accent))" stopOpacity="0" />
              </linearGradient>
            </defs>
            <motion.path
              d="M 3 0 L 3 100"
              stroke="url(#hiw-line-v)"
              strokeWidth="1"
              fill="none"
              vectorEffect="non-scaling-stroke"
              initial={{ pathLength: 0 }}
              whileInView={{ pathLength: 1 }}
              viewport={inViewOnce}
              transition={drawTransition}
              style={{ filter: "drop-shadow(0 0 6px hsl(var(--primary)/0.5))" }}
            />
          </svg>

          <ol className="relative z-10 grid grid-cols-1 gap-12 md:grid-cols-3 md:gap-8">
            {steps.map((step) => {
              const Icon = ICONS[step.icon] ?? ShieldCheck;
              return (
                <motion.li
                  key={step.number}
                  variants={fadeInUp}
                  className="flex items-start gap-5 md:flex-col md:items-center md:text-center"
                >
                  {/* Icon circle. Solid background so the connector line gets
                      visually clipped at the edge of each "node". */}
                  <div
                    className="relative flex h-[88px] w-[88px] shrink-0 items-center justify-center rounded-full border border-primary/30 bg-background"
                    aria-hidden="true"
                  >
                    <div className="absolute inset-1 rounded-full bg-gradient-to-br from-primary/20 to-accent/20" />
                    <Icon className="relative h-7 w-7 text-primary" />
                  </div>

                  <div className="md:mt-6">
                    <p
                      className="font-mono text-3xl font-semibold tracking-tight text-primary md:text-4xl"
                      style={{
                        textShadow: "0 0 24px hsl(var(--primary) / 0.45)",
                      }}
                    >
                      {step.number}
                    </p>
                    <h3 className="mt-2 font-heading text-xl font-semibold tracking-tight text-foreground md:text-2xl">
                      {step.title}
                    </h3>
                    <p className="mt-2 max-w-md text-sm leading-relaxed text-muted-foreground md:text-base">
                      {step.description}
                    </p>
                  </div>
                </motion.li>
              );
            })}
          </ol>
        </motion.div>
      </div>
    </section>
  );
}
