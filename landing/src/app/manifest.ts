import type { MetadataRoute } from "next";

export const dynamic = "force-static";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Rise VPN",
    short_name: "Rise",
    description: "Свобода интернета без границ",
    start_url: "/ru/",
    display: "standalone",
    background_color: "#030711",
    theme_color: "#030711",
    icons: [
      // SVG covers any size on Android Chrome (>=2018) and modern desktop
      // browsers. The /icon route generated from src/app/icon.tsx provides
      // a 32×32 PNG fallback for older clients; /apple-icon adds 180×180
      // for iOS Add-to-Home-Screen.
      { src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any" },
      { src: "/icon", sizes: "32x32", type: "image/png" },
      { src: "/apple-icon", sizes: "180x180", type: "image/png" },
    ],
  };
}
