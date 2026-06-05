# Plorigo brand assets

Official Plorigo logos and icons. These are **trademarks** — usage is governed by
[TRADEMARK.md](../../TRADEMARK.md). In short: use them to refer to or link to Plorigo; don't
use them to brand a fork or imply official endorsement.

**Tagline:** _Launch with control._

## What the mark means

The "P" mark is built from four ideas that define the product:

- **Letter P** — the platform itself.
- **Arrow / launch** — deploying and shipping.
- **Shield / safety** — built-in production guardrails.
- **Server stack** — infrastructure you own.

See [`plorigo-brand-overview.png`](./plorigo-brand-overview.png) for the full brand sheet
(logo on light/dark, app-icon variations, mockups, and meaning).

## Logo (icon + wordmark)

Use the variant that matches the background. Both are transparent PNGs with the same crop.

| File | Use on |
|---|---|
| [`plorigo-logo-black.png`](./plorigo-logo-black.png) | light backgrounds |
| [`plorigo-logo-white.png`](./plorigo-logo-white.png) | dark backgrounds |
| [`plorigo-logo-black-on-white.png`](./plorigo-logo-black-on-white.png) | original supplied lockup (black on a white tile) |

For theme-aware rendering in Markdown:

```html
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/brand/plorigo-logo-white.png">
  <img alt="Plorigo" src="assets/brand/plorigo-logo-black.png" width="340">
</picture>
```

## Icon / mark (square)

The "P" mark — for avatars, app icons, favicons, and anywhere a square mark is needed.

| File | Notes |
|---|---|
| [`plorigo-icon.png`](./plorigo-icon.png) | primary, gradient (1254×1254) |
| [`plorigo-icon-black.png`](./plorigo-icon-black.png) | monochrome |

## App icons

Pre-generated from the gradient mark, ready to drop into the dashboard's `public/` when
`apps/web` is scaffolded:

```
app-icons/
├── favicon.ico            # 16/32/48 multi-size
├── icon-16.png
├── icon-32.png
├── icon-192.png           # PWA
├── icon-512.png           # PWA
└── apple-touch-icon.png   # 180×180
```

## Social preview

[`social-preview.png`](./social-preview.png) — 1280×640, for GitHub's repository social
preview (Settings → General → Social preview) and link unfurls.

## Colors

The mark uses a purple→blue gradient (approx. `#7C5CFC` → `#3B82F6`). The dark surfaces use
`#0D1117`.

---

Need a format we don't have here (SVG, a specific size)? Open an issue or email
**i.babirli@outlook.com**.
