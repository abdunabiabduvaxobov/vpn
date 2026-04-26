import type { SVGProps } from "react";

/**
 * Inline brand-mark icons. Lucide v1 removed all brand glyphs (trademark
 * concerns), and we only need a handful in the footer / contact strip, so
 * adding a dedicated brand-icon dependency would be overkill. Each glyph is
 * authored at 24×24 to drop into Lucide-styled spots without code changes.
 */

type Props = SVGProps<SVGSVGElement>;

function Base({ children, ...props }: Props & { children: React.ReactNode }) {
  return (
    <svg
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="currentColor"
      aria-hidden="true"
      {...props}
    >
      {children}
    </svg>
  );
}

export function TelegramIcon(props: Props) {
  return (
    <Base {...props}>
      <path d="M21.85 4.42a1 1 0 0 0-1.41-1.05L2.86 10.7a1 1 0 0 0 .07 1.86l4.24 1.34 1.66 5.27a1 1 0 0 0 1.66.45l2.41-2.06 4.21 3.07a1 1 0 0 0 1.55-.6l3.19-15.61ZM10 14.41l-.32 3 1.62-1.39 6.95-6.69-8.25 5.08Z" />
    </Base>
  );
}

export function XIcon(props: Props) {
  return (
    <Base {...props}>
      <path d="M17.53 3H20.7l-6.93 7.92L22 21h-6.4l-5.01-6.55L4.86 21H1.69l7.42-8.48L1.6 3h6.56l4.53 5.99L17.53 3Zm-1.12 16.2h1.76L7.66 4.7H5.78L16.41 19.2Z" />
    </Base>
  );
}

export function YouTubeIcon(props: Props) {
  return (
    <Base {...props}>
      <path d="M23.5 6.5a3 3 0 0 0-2.1-2.13C19.55 4 12 4 12 4s-7.55 0-9.4.37A3 3 0 0 0 .5 6.5C.13 8.36.13 12 .13 12s0 3.64.37 5.5a3 3 0 0 0 2.1 2.13C4.45 20 12 20 12 20s7.55 0 9.4-.37a3 3 0 0 0 2.1-2.13c.37-1.86.37-5.5.37-5.5s0-3.64-.37-5.5ZM9.75 15.5v-7l6.5 3.5-6.5 3.5Z" />
    </Base>
  );
}

export function GitHubIcon(props: Props) {
  return (
    <Base {...props}>
      <path d="M12 1.27a11 11 0 0 0-3.48 21.43c.55.1.75-.24.75-.53v-1.85c-3.07.67-3.71-1.48-3.71-1.48-.5-1.27-1.22-1.61-1.22-1.61-1-.68.08-.67.08-.67 1.1.08 1.68 1.13 1.68 1.13.98 1.68 2.57 1.2 3.2.92.1-.71.38-1.2.7-1.48-2.45-.28-5.03-1.23-5.03-5.46 0-1.21.43-2.2 1.13-2.97-.11-.28-.49-1.4.11-2.92 0 0 .93-.3 3.04 1.13a10.5 10.5 0 0 1 5.53 0c2.11-1.43 3.03-1.13 3.03-1.13.6 1.52.22 2.64.11 2.92.7.77 1.13 1.76 1.13 2.97 0 4.24-2.59 5.18-5.05 5.45.4.34.74 1 .74 2.04v3.03c0 .29.2.64.76.53A11 11 0 0 0 12 1.27Z" />
    </Base>
  );
}
