"use client";

import { useTranslations } from "next-intl";
import { motion, useReducedMotion } from "motion/react";
import { ArrowRight, EyeOff, Globe, Lock, Zap } from "lucide-react";
import { AnimatedGrid } from "@/components/effects/animated-grid";
import { GlowOrb } from "@/components/effects/glow-orb";
import { GradientMesh } from "@/components/effects/gradient-mesh";
import { NoiseOverlay } from "@/components/effects/noise-overlay";
import { buttonVariants } from "@/components/ui/button";
import { fadeInUp, staggerContainer } from "@/lib/animations";
import { APP_DOWNLOAD } from "@/lib/constants";

const TRUST_BADGES = [
  { icon: Lock, key: "encryption" },
  { icon: Globe, key: "servers" },
  { icon: Zap, key: "speed" },
  { icon: EyeOff, key: "noLogs" },
] as const;

export function Hero() {
  const t = useTranslations("hero");
  const reduceMotion = useReducedMotion();

  // When the user requests reduced motion, replace stagger fade-in with an
  // immediate fade so the page still settles cleanly but doesn't move.
  const initial = reduceMotion ? "visible" : "hidden";

  return (
    <section className="relative isolate flex min-h-[calc(100vh-4rem)] flex-col items-center justify-center overflow-hidden px-6 py-24 text-center">
      {/* Background stack — z-index lives on the parent's `isolate` so the
          stack stays behind the foreground regardless of section position. */}
      <GradientMesh className="z-0" />
      <AnimatedGrid className="z-0" />
      <GlowOrb
        color="primary"
        size={680}
        className="-right-40 -top-40 z-0"
        opacity={0.4}
      />
      <GlowOrb
        color="accent"
        size={520}
        className="-bottom-40 -left-32 z-0"
        opacity={0.3}
      />
      <NoiseOverlay className="z-0" />

      {/* Foreground */}
      <motion.div
        variants={staggerContainer}
        initial={initial}
        animate="visible"
        className="relative z-10 mx-auto flex max-w-4xl flex-col items-center"
      >
        <motion.p
          variants={fadeInUp}
          className="font-mono text-xs uppercase tracking-[0.2em] text-primary md:text-sm"
        >
          {t("eyebrow")}
        </motion.p>

        <motion.h1
          variants={fadeInUp}
          className="mt-6 max-w-3xl font-heading text-4xl font-semibold leading-[1.1] tracking-tight md:text-6xl lg:text-7xl"
        >
          {t.rich("title", {
            hl: (chunks) => (
              <span className="bg-gradient-to-r from-primary via-primary-glow to-accent bg-clip-text text-transparent">
                {chunks}
              </span>
            ),
          })}
        </motion.h1>

        <motion.p
          variants={fadeInUp}
          className="mt-8 max-w-2xl text-base text-muted-foreground md:text-lg"
        >
          {t("subtitle")}
        </motion.p>

        <motion.div
          variants={fadeInUp}
          className="mt-10 flex flex-col items-center gap-3 sm:flex-row sm:gap-4"
        >
          <a
            href={APP_DOWNLOAD.android}
            className={
              buttonVariants({ size: "lg" }) +
              " group h-12 rounded-full px-7 text-base shadow-[0_0_40px_-10px_hsl(var(--primary)/0.6)] transition-shadow hover:shadow-[0_0_60px_-8px_hsl(var(--primary)/0.8)]"
            }
          >
            {t("ctaPrimary")}
            <ArrowRight className="ml-2 h-4 w-4 transition-transform group-hover:translate-x-0.5" />
          </a>
          <a
            href="#how-it-works"
            className={
              buttonVariants({ variant: "ghost", size: "lg" }) +
              " h-12 rounded-full px-6 text-base text-muted-foreground hover:text-foreground"
            }
          >
            {t("ctaSecondary")}
          </a>
        </motion.div>

        <motion.ul
          variants={fadeInUp}
          aria-label={t("eyebrow")}
          className="mt-12 flex flex-wrap items-center justify-center gap-2 md:gap-3"
        >
          {TRUST_BADGES.map(({ icon: Icon, key }) => (
            <li
              key={key}
              className="inline-flex items-center gap-2 rounded-full border border-border-subtle bg-surface/60 px-4 py-2 text-sm text-muted-foreground backdrop-blur"
            >
              <Icon className="h-4 w-4 text-primary" />
              <span>{t(`trust.${key}`)}</span>
            </li>
          ))}
        </motion.ul>
      </motion.div>
    </section>
  );
}
