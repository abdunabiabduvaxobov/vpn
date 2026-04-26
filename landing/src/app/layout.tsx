/**
 * Root layout — intentionally minimal. The actual <html>, <body>, fonts, and
 * locale provider live in `src/app/[locale]/layout.tsx`. Next.js requires a
 * top-level layout file even when the locale segment fully owns the document
 * shell, so this is a pass-through.
 */
export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return children;
}
