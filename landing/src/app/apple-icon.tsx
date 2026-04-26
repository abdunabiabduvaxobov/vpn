import { ImageResponse } from "next/og";

export const size = { width: 180, height: 180 };
export const contentType = "image/png";
export const dynamic = "force-static";

/**
 * Apple touch icon — used by iOS when the user adds the site to their
 * home screen. Larger and with rounded corners that iOS itself does not
 * apply, so we render them inline (matches Android's PWA install treatment).
 */
export default function AppleIcon() {
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
          borderRadius: "40px",
        }}
      >
        <svg viewBox="0 0 512 512" width="120" height="120">
          <defs>
            <linearGradient id="apple-grad" x1="0" y1="0" x2="1" y2="1">
              <stop offset="0%" stopColor="#3B82F6" />
              <stop offset="100%" stopColor="#06B6D4" />
            </linearGradient>
          </defs>
          <path
            d="M256 64 L432 168 V288 C432 384 352 448 256 464 C160 448 80 384 80 288 V168 Z"
            fill="none"
            stroke="url(#apple-grad)"
            strokeWidth="32"
            strokeLinejoin="round"
          />
          <path
            d="M168 264 L232 328 L344 200"
            fill="none"
            stroke="url(#apple-grad)"
            strokeWidth="36"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </div>
    ),
    { ...size },
  );
}
