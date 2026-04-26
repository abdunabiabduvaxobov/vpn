/**
 * Procedural film-grain overlay via SVG <feTurbulence>. Inlined as a
 * background-image data URL so it ships in HTML/CSS only, no extra request.
 * Opacity stays low (0.03) so it adds texture without reading as noise.
 */
export function NoiseOverlay({ className }: { className?: string }) {
  const svg = `
    <svg xmlns='http://www.w3.org/2000/svg' width='240' height='240'>
      <filter id='n'>
        <feTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2' stitchTiles='stitch'/>
        <feColorMatrix values='0 0 0 0 1 0 0 0 0 1 0 0 0 0 1 0 0 0 0.6 0'/>
      </filter>
      <rect width='100%' height='100%' filter='url(%23n)'/>
    </svg>`;
  const dataUrl = `url("data:image/svg+xml;utf8,${encodeURIComponent(svg)}")`;

  return (
    <div
      aria-hidden="true"
      className={`pointer-events-none absolute inset-0 mix-blend-overlay ${className ?? ""}`}
      style={{
        backgroundImage: dataUrl,
        backgroundSize: "240px 240px",
        opacity: 0.03,
      }}
    />
  );
}
