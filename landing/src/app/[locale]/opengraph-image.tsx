import { ImageResponse } from "next/og";
import { routing } from "@/i18n/routing";

export const alt = "Rise VPN — internet freedom without borders";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const dynamic = "force-static";

export function generateStaticParams() {
  return routing.locales.map((locale) => ({ locale }));
}

/**
 * Branded social-share image. Brand-mark + wordmark only — we deliberately
 * skip locale-specific copy here so we don't have to ship a Cyrillic font
 * with `fetch` at build time. The mark + the name + the URL is enough for
 * a Twitter / Telegram preview, and the locale page's metadata still
 * carries the localized title in the `og:title` text alongside this image.
 */
export default async function OgImage() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          alignItems: "center",
          justifyContent: "center",
          backgroundColor: "#030711",
          backgroundImage: [
            "radial-gradient(circle at 25% 25%, rgba(37, 99, 235, 0.35), transparent 45%)",
            "radial-gradient(circle at 75% 75%, rgba(6, 182, 212, 0.30), transparent 45%)",
          ].join(", "),
          color: "#F1F5F9",
          fontFamily: "system-ui, -apple-system, sans-serif",
        }}
      >
        {/* Subtle grid backdrop. SVG works inside ImageResponse's satori. */}
        <svg
          width={size.width}
          height={size.height}
          style={{ position: "absolute", inset: 0, opacity: 0.18 }}
        >
          <defs>
            <pattern
              id="grid"
              width="64"
              height="64"
              patternUnits="userSpaceOnUse"
            >
              <path
                d="M 64 0 L 0 0 0 64"
                fill="none"
                stroke="#1E3A5F"
                strokeWidth="1"
              />
            </pattern>
          </defs>
          <rect width="100%" height="100%" fill="url(#grid)" />
        </svg>

        {/* Brand mark — gradient shield */}
        <svg
          viewBox="0 0 512 512"
          width="160"
          height="160"
          style={{ marginBottom: "24px" }}
        >
          <defs>
            <linearGradient id="og-shield" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0%" stopColor="#3B82F6" />
              <stop offset="100%" stopColor="#06B6D4" />
            </linearGradient>
          </defs>
          <path
            d="M256 64 L432 168 V288 C432 384 352 448 256 464 C160 448 80 384 80 288 V168 Z"
            fill="none"
            stroke="url(#og-shield)"
            strokeWidth="32"
            strokeLinejoin="round"
          />
          <path
            d="M168 264 L232 328 L344 200"
            fill="none"
            stroke="url(#og-shield)"
            strokeWidth="36"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>

        <div
          style={{
            fontSize: "128px",
            fontWeight: 700,
            letterSpacing: "-0.04em",
            background: "linear-gradient(90deg, #3B82F6, #06B6D4)",
            backgroundClip: "text",
            color: "transparent",
            display: "flex",
          }}
        >
          Rise VPN
        </div>

        <div
          style={{
            marginTop: "16px",
            fontSize: "32px",
            fontFamily: "ui-monospace, monospace",
            color: "#94A3B8",
            display: "flex",
          }}
        >
          vpn.mydayai.uz
        </div>
      </div>
    ),
    { ...size },
  );
}
