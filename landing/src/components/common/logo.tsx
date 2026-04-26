import { SITE } from "@/lib/constants";

/**
 * Brand mark — a shield with a checkmark, gradient stroke. The same shape
 * lives in /public/icon.svg so the app icon, manifest icon, and the in-page
 * logo all match. Inline SVG (no <Image>) so the gradient picks up real CSS
 * variables when we later want to retheme.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 ${className ?? ""}`}>
      <svg
        viewBox="0 0 512 512"
        className="h-7 w-7"
        aria-hidden="true"
        fill="none"
      >
        <defs>
          <linearGradient id="logo-gradient" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stopColor="hsl(var(--primary))" />
            <stop offset="100%" stopColor="hsl(var(--accent))" />
          </linearGradient>
        </defs>
        <path
          d="M256 64 L432 168 V288 C432 384 352 448 256 464 C160 448 80 384 80 288 V168 Z"
          stroke="url(#logo-gradient)"
          strokeWidth="32"
          strokeLinejoin="round"
        />
        <path
          d="M168 264 L232 328 L344 200"
          stroke="url(#logo-gradient)"
          strokeWidth="36"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      <span className="font-heading text-lg font-semibold tracking-tight">
        {SITE.name}
      </span>
    </span>
  );
}
