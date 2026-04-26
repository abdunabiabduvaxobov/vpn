"use client";

import { useTranslations } from "next-intl";
import { motion, useReducedMotion } from "motion/react";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { fadeInUp, inViewOnce, staggerContainer } from "@/lib/animations";
import { faqSchema } from "@/lib/seo";
import { SOCIAL_LINKS } from "@/lib/constants";

type Item = { q: string; a: string };

export function FAQ() {
  const t = useTranslations("faq");
  const reduceMotion = useReducedMotion();
  const items = (t.raw("items") ?? []) as Item[];

  // FAQPage structured data — colocated with the visible Q&As so the schema
  // can never silently drift from the rendered content. Pre-rendered into
  // the static HTML; Google's crawler sees it on first byte.
  const ld = faqSchema(items);

  return (
    <section
      id="faq"
      className="relative overflow-hidden border-t border-border-subtle bg-background py-24 md:py-32"
    >
      <div className="mx-auto max-w-3xl px-4 md:px-6 lg:px-8">
        <motion.div
          variants={fadeInUp}
          initial={reduceMotion ? "visible" : "hidden"}
          whileInView="visible"
          viewport={inViewOnce}
          className="text-center"
        >
          <p className="font-mono text-xs uppercase tracking-[0.2em] text-primary">
            {t("eyebrow")}
          </p>
          <h2 className="mt-4 font-heading text-3xl font-semibold tracking-tight md:text-5xl">
            {t("title")}
          </h2>
          <p className="mt-4 text-base text-muted-foreground md:text-lg">
            {t.rich("subtitle", {
              tg: (chunks) => (
                <a
                  href={SOCIAL_LINKS.telegram}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-medium text-primary underline-offset-4 transition hover:text-primary-glow hover:underline"
                >
                  {chunks}
                </a>
              ),
            })}
          </p>
        </motion.div>

        <motion.div
          variants={staggerContainer}
          initial={reduceMotion ? "visible" : "hidden"}
          whileInView="visible"
          viewport={inViewOnce}
          className="mt-12"
        >
          <Accordion
            // base-ui's accordion supports `multiple={false}` for single-open
            // behaviour. Default value is undefined → no item open initially.
            className="space-y-3"
          >
            {items.map((item, i) => (
              <motion.div key={item.q} variants={fadeInUp}>
                <AccordionItem
                  value={`item-${i}`}
                  className="rounded-xl border border-border-subtle bg-surface/40 px-5 backdrop-blur-xl transition-colors hover:border-primary/30 data-[panel-open]:border-primary/40"
                >
                  <AccordionTrigger className="font-heading text-base font-medium text-foreground hover:no-underline md:text-lg">
                    {item.q}
                  </AccordionTrigger>
                  <AccordionContent className="text-base leading-relaxed text-muted-foreground">
                    {item.a}
                  </AccordionContent>
                </AccordionItem>
              </motion.div>
            ))}
          </Accordion>
        </motion.div>
      </div>

      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(ld) }}
      />
    </section>
  );
}
