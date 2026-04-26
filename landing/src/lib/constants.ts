export const SITE = {
  name: "Rise VPN",
  url: "https://vpn.mydayai.uz",
  tagline: "Свобода интернета без границ",
} as const;

export const APP_DOWNLOAD = {
  // Until Play/App Store listings are live, link to the direct APK so the
  // landing's primary CTA still gives users a working install path.
  android: "https://vpnapi.mydayai.uz:9443/vpn-release.apk",
  // Placeholders — fill once Play Store / App Store / desktop builds ship.
  ios: "#",
  windows: "#",
  macos: "#",
  linux: "#",
} as const;

export const SOCIAL_LINKS = {
  telegram: "https://t.me/flawlssr",
  twitter: "#",
  youtube: "#",
  vk: "#",
  github: "#",
} as const;

export const SUPPORT = {
  email: "support@vpn.mydayai.uz",
  telegram: "https://t.me/flawlssr",
} as const;
