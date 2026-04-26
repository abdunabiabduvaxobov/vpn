/**
 * Atmospheric backdrop made of three overlapping radial gradients with low
 * alpha. Lives behind everything else so the page feels lit from inside the
 * background instead of flat-coloured. No animation — keeps the GPU free for
 * the hero text and orbs.
 */
export function GradientMesh({ className }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={`pointer-events-none absolute inset-0 ${className ?? ""}`}
      style={{
        backgroundImage: [
          "radial-gradient(circle at 20% 20%, hsl(var(--primary) / 0.15), transparent 45%)",
          "radial-gradient(circle at 80% 30%, hsl(var(--accent) / 0.12), transparent 40%)",
          "radial-gradient(circle at 50% 90%, hsl(var(--primary-glow) / 0.10), transparent 50%)",
        ].join(", "),
      }}
    />
  );
}
