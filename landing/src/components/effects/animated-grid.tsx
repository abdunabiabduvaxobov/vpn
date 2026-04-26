/**
 * SVG grid drawn with a <pattern>, masked by a radial gradient so it fades
 * out at the edges. Pure CSS keyframes drift it horizontally — keeps this a
 * server component (no client JS) and respects prefers-reduced-motion via
 * the `motion-reduce:animate-none` Tailwind variant.
 */
export function AnimatedGrid({
  className,
  size = 56,
}: {
  className?: string;
  size?: number;
}) {
  const id = "rv-grid";
  const maskId = "rv-grid-mask";

  return (
    <div
      aria-hidden="true"
      className={`pointer-events-none absolute inset-0 overflow-hidden ${className ?? ""}`}
    >
      <svg
        className="h-full w-full motion-reduce:animate-none"
        style={{ animation: "rv-grid-drift 60s linear infinite" }}
        xmlns="http://www.w3.org/2000/svg"
      >
        <defs>
          <pattern
            id={id}
            width={size}
            height={size}
            patternUnits="userSpaceOnUse"
          >
            <path
              d={`M ${size} 0 L 0 0 0 ${size}`}
              fill="none"
              stroke="hsl(var(--border))"
              strokeOpacity="0.4"
              strokeWidth="1"
            />
          </pattern>
          <radialGradient id={maskId} cx="50%" cy="50%" r="55%">
            <stop offset="0%" stopColor="white" stopOpacity="1" />
            <stop offset="70%" stopColor="white" stopOpacity="0.5" />
            <stop offset="100%" stopColor="white" stopOpacity="0" />
          </radialGradient>
          <mask id="rv-grid-fade">
            <rect width="100%" height="100%" fill={`url(#${maskId})`} />
          </mask>
        </defs>
        <rect
          width="200%"
          height="100%"
          fill={`url(#${id})`}
          mask="url(#rv-grid-fade)"
        />
      </svg>

      <style>{`
        @keyframes rv-grid-drift {
          from { transform: translateX(0); }
          to   { transform: translateX(-${size * 2}px); }
        }
      `}</style>
    </div>
  );
}
