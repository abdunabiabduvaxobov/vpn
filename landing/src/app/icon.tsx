import { ImageResponse } from "next/og";

export const size = { width: 32, height: 32 };
export const contentType = "image/png";
// Static export — must be flagged as fully static so it generates at build time.
export const dynamic = "force-static";

/**
 * Favicon — gradient shield mark, scaled down to 32×32. Generated at build
 * time so it ships as a real PNG (not the .ico that create-next-app put in
 * src/app/favicon.ico, which we leave in place as the legacy fallback).
 */
export default function Icon() {
  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          background: "#030711",
          borderRadius: "8px",
        }}
      >
        <svg viewBox="0 0 512 512" width="26" height="26">
          <defs>
            <linearGradient id="favicon-grad" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0%" stopColor="#3B82F6" />
              <stop offset="100%" stopColor="#06B6D4" />
            </linearGradient>
          </defs>
          <path
            d="M256 64 L432 168 V288 C432 384 352 448 256 464 C160 448 80 384 80 288 V168 Z"
            fill="none"
            stroke="url(#favicon-grad)"
            strokeWidth="48"
            strokeLinejoin="round"
          />
        </svg>
      </div>
    ),
    { ...size },
  );
}
