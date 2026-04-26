type Color = "primary" | "accent";

/**
 * Soft radial-gradient blob, blurred to act as a backlight. Position via
 * Tailwind utilities on the wrapper, control intensity with `opacity` and
 * `blur`. CSS keyframes provide a slow pulse; motion-reduce disables it.
 */
export function GlowOrb({
  color = "primary",
  size = 600,
  blur = 120,
  opacity = 0.35,
  className,
  pulse = true,
}: {
  color?: Color;
  size?: number;
  blur?: number;
  opacity?: number;
  className?: string;
  pulse?: boolean;
}) {
  const colorVar = color === "accent" ? "var(--accent)" : "var(--primary)";

  return (
    <div
      aria-hidden="true"
      className={`pointer-events-none absolute ${className ?? ""}`}
      style={{
        width: size,
        height: size,
        opacity,
        background: `radial-gradient(closest-side, hsl(${colorVar} / 0.9), hsl(${colorVar} / 0) 70%)`,
        filter: `blur(${blur}px)`,
        animation: pulse ? "rv-orb-pulse 6s ease-in-out infinite" : undefined,
      }}
    >
      <style>{`
        @media (prefers-reduced-motion: reduce) {
          [style*="rv-orb-pulse"] { animation: none !important; }
        }
        @keyframes rv-orb-pulse {
          0%, 100% { transform: scale(1); opacity: var(--rv-orb-base, 1); }
          50% { transform: scale(1.08); opacity: calc(var(--rv-orb-base, 1) * 1.15); }
        }
      `}</style>
    </div>
  );
}
