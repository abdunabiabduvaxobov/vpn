# Brand-mark assets

This directory holds the third-party vendor logomarks required by Phase 4's
`/login` page (Plan 04). Each SVG is rendered inside a button labelled "Sign
in with Apple" or "Sign in with Google" — the wrapping React component lives
in `landing/src/components/app/oauth-buttons.tsx` (Plan 04).

## Assets

| Path | Source | License notes |
|------|--------|---------------|
| `apple/apple-sign-in.svg` | Apple HIG — Sign in with Apple button assets, <https://developer.apple.com/design/human-interface-guidelines/sign-in-with-apple> | Apple permits the Apple logomark **only** on a "Sign in with Apple" button. Do not reuse it anywhere else in the UI. Apple's official downloadable button package (PDF/SwiftUI bundle) supersedes this file — refresh from the HIG page if Apple updates their guidelines. |
| `google/google-g.svg` | Google Identity branding guidelines — `Google_G_Logo.svg`, <https://developers.google.com/identity/branding-guidelines#logo> | Google permits the "G" logomark **only** on a "Sign in with Google" button at the documented padding/sizing. The official downloadable asset from the link above supersedes this file. |

## File state

Both files in this commit are **faithful SVG renditions** of the official
brand marks (the geometric shapes documented in the public guidelines) so the
build is offline-reproducible. Before any production push the operator MUST
either:

1. Confirm these renditions still match the current Apple/Google guidelines
   (no glyph or palette change in the upstream brand kits), OR
2. Replace each file with the byte-identical asset downloaded from the
   official source URLs above.

The replacement procedure: drop the new SVG at the same path, keep the
existing filename (the Plan 04 React component imports the path as a string
literal), commit with message `chore(landing): refresh {apple|google} brand
asset to upstream YYYY-MM-DD`.

## Why we vendor instead of `<img>`-loading from a CDN

- **No external dependency at runtime** — the `/login` page renders inside
  the (app) route group with `dynamic = 'force-dynamic'`; a CDN outage would
  block the sign-in button.
- **Lighthouse + CSP** — local assets pass the strict `img-src 'self'`
  Content Security Policy that Phase 4 will land in Plan 08.
- **Predictable bundle size** — both files are well under 4 KB and Next.js
  inlines `<Image src=>` references with a fingerprinted URL.

## When Apple/Google update their brand guidelines

Re-download the upstream asset (URLs above), drop into the same path, and
update the License notes column above if the terms change. Verify with:

```bash
head -1 landing/public/brand/apple/apple-sign-in.svg | grep -q "<svg"
head -1 landing/public/brand/google/google-g.svg | grep -q "<svg"
```
