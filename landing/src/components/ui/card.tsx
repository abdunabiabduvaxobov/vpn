import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * Card — surface container used by /login, /dashboard, /pricing, /pay/success,
 * /pay/fail. Pure styled div primitives; no client directive so server
 * components can render them freely. Tokens reference the values already
 * defined in `globals.css` (Phase 4 D-05 — no shadcn install). Border + radius
 * + bg are all aliased to CSS variables so retheming touches one place.
 */

type DivProps = React.HTMLAttributes<HTMLDivElement>;
type HeadingProps = React.HTMLAttributes<HTMLHeadingElement>;

const Card = React.forwardRef<HTMLDivElement, DivProps>(function Card(
  { className, ...props },
  ref,
) {
  return (
    <div
      ref={ref}
      data-slot="card"
      className={cn(
        "rounded-[var(--radius-xl)] border border-border-subtle bg-surface p-6 text-card-foreground shadow-sm",
        className,
      )}
      {...props}
    />
  );
});

const CardHeader = React.forwardRef<HTMLDivElement, DivProps>(
  function CardHeader({ className, ...props }, ref) {
    return (
      <div
        ref={ref}
        data-slot="card-header"
        className={cn("flex flex-col gap-1.5 pb-4", className)}
        {...props}
      />
    );
  },
);

const CardTitle = React.forwardRef<HTMLHeadingElement, HeadingProps>(
  function CardTitle({ className, ...props }, ref) {
    return (
      <h2
        ref={ref}
        data-slot="card-title"
        className={cn(
          "font-heading text-2xl font-semibold tracking-tight",
          className,
        )}
        {...props}
      />
    );
  },
);

const CardDescription = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLParagraphElement>>(
  function CardDescription({ className, ...props }, ref) {
    return (
      <p
        ref={ref}
        data-slot="card-description"
        className={cn("text-sm text-muted-foreground", className)}
        {...props}
      />
    );
  },
);

const CardContent = React.forwardRef<HTMLDivElement, DivProps>(
  function CardContent({ className, ...props }, ref) {
    return (
      <div
        ref={ref}
        data-slot="card-content"
        className={cn("flex flex-col gap-4", className)}
        {...props}
      />
    );
  },
);

const CardFooter = React.forwardRef<HTMLDivElement, DivProps>(
  function CardFooter({ className, ...props }, ref) {
    return (
      <div
        ref={ref}
        data-slot="card-footer"
        className={cn("flex items-center justify-end gap-2 pt-4", className)}
        {...props}
      />
    );
  },
);

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter };
