# Lumen identity

Lumen's mark is a blue photographic aperture opening onto a warm illuminated **L**. It connects the app's name to light and photography, with a compact silhouette for desktop icons. The brand colours stay fixed when users change the UI theme or accent; interface controls continue to use the existing semantic design tokens.

## Source and use

- `build/appicon.png`: canonical 1254 × 1254 transparent PNG master. Generated with the built-in image-generation tool on 2026-10-05; retain this source rather than editing derived files. This is raster artwork, not an SVG or vector source.
- `frontend/public/lumen.png`: 128 × 128, shared by `BrandMark.vue` and the native application About icon. Covers the sidebar, collapsed rail, navigation drawer, narrow header, onboarding and Settings → About. The app name remains live, theme-aware system-font text. Marks adjacent to that name are decorative; a collapsed standalone mark has an accessible name.
- `frontend/public/favicon.png`: dedicated 32 × 32 browser icon.
- `build/windows/icon.ico`: 16, 32, 48, 64, 128 and 256 pixel representations, embedded into Lumen.exe by the existing syso build and into NSIS setup/uninstall; shortcuts use the executable icon.
- `build/darwin/icons.icns`, DMG icon/background and `build/appicon.icon`: the same identity for the macOS build templates. The stale template `Assets.car` was removed and is now ignored; Icon Composer regenerates it on macOS 26+. No Wails artwork remains in the branding sources.
- `build/linux/lumen.png`: correctly sized 128 × 128 packaged hicolor icon; AppImage uses the master.
- `build/ios/icon.png` and Android mipmap PNGs: derived sources for the mobile templates. iOS icons have opaque neutral backgrounds; the Xcode task derives sizes from Wails' generated `Contents.json` and replaces its transparent exports.

Keep the aperture's proportions and colours. Do not stretch, recolour to a chosen UI accent, add a badge, or place it over photos. Keep visible branding at app-identity surfaces; ordinary controls retain the bundled Lucide icons.

## Regeneration

Run from the repository root in Git Bash:

```sh
wails3 task common:generate:icons
```

This runs `go run ./scripts/branding` and then the existing Wails ICO/ICNS generator. The frontend and icon build tasks depend on the same PNG task, with `run: once` preventing concurrent writers. PNGs use the existing `golang.org/x/image` scaler and preserve transparency, with no new packages, image service, runtime dependency or network request. Identical PNGs are left untouched. iOS exports follow its generated asset catalogue rather than a second size manifest. The branding command is included in `scripts/lint.sh`.

After changing the master, regenerate and commit all derived assets together. Run `scripts/lint.sh`, `wails3 task package`, and inspect native onboarding, navigation and About in both themes. macOS, Linux and mobile assets can be generated here, but acceptance of those applications requires their native toolchains/devices.

## Verification on 2026-10-05

- `scripts/lint.sh`: passed, including Go checks, vulnerability scanning, frontend type/lint/format checks, all 507 frontend tests, the branding command, installer regressions and ShellCheck.
- `wails3 task package`: built `bin/Lumen.exe` and `bin/Lumen-0.10.0-x64-setup.exe`. Extracted Windows icons from both executables show the new aperture mark. This is an unsigned local build; installation on a clean machine was not repeated for this branding change.
- PNG verification checked the master, UI, favicon, Linux and Icon Composer dimensions, transparent corners and brand colours; all six ICO representations retain colour and transparency. The iOS catalogue exported all 15 PNGs with no transparent pixels.
- Native WebView2: [18 checks passed](branding-native-acceptance-2026-10-05.json), with no JavaScript errors. Onboarding, sidebar, collapsed rail, About, narrow header and navigation drawer were exercised in light and dark themes. Screenshots were inspected after transitions settled. Fixtures, data and screenshots are isolated under `D:/tmp/lumen-branding-2026-10-05`; the final run used DevTools port 9431 and the test app was closed.
- Generated bindings and dependency lockfiles have no drift. macOS, Linux and mobile runtime acceptance remains unverified on this Windows machine.
- Regenerating every PNG produced identical SHA-256 hashes. The production npm dependency audit has zero findings; the existing development-tool advisory recorded in the Phase 7 review remains unchanged.

Local build SHA-256:

```text
Lumen.exe: 3C89BD1A4F19294DECE6BB66F960992F8136F71995309C058DDC4FECDDB8EF6F
Lumen-0.10.0-x64-setup.exe: E2BA8CC914F336F9AB0A0BBDDBB66E5DEA5AB98AC5D36A77B18E775692AAB9F7
```

## Generation prompt

```text
Create a professional, distinctive application logo SYMBOL for Lumen, a private offline desktop photo and video library. This is a production app icon master, not a presentation sheet.
Design idea: a luminous photographic aperture whose broad geometric blades subtly form a capital L through negative space; light revealed and memories brought into focus. Invent a simple ownable silhouette, avoid a literal camera pictogram. Calm premium photographic software identity.
One single centered symbol occupying about 86 percent of a square canvas, transparent outside. Use a deep midnight navy circular or softly rounded aperture silhouette, a few substantial clean overlapping blades in rich cobalt blue and pale azure, and one restrained warm gold illuminated opening. Keep it flat, precise vector-like artwork with very subtle tonal depth only, bold broad forms, coherent geometry, generous negative space, polished professional visual balance. Needs to look crisp and legible at 16, 24, 32 and 48 pixels against white AND charcoal backgrounds.
No lettering, no wordmark, no text, no mockups, no scene, no bounding square tile behind the symbol, no drop shadow, no outer glow, no thin outlines, no tiny details, no generic AI sparkle/star, no rainbow gradients, no watermarks. True transparent background and clean antialiased alpha edge. Output a high resolution square PNG.
```
