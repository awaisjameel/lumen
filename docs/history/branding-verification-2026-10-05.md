# Branding verification, 5 October 2026

Historical evidence moved from [the living branding guide](../branding.md) during
the documentation refresh. These results and hashes describe that dated build.

- `scripts/lint.sh`: passed, including Go checks, vulnerability scanning, frontend type/lint/format checks, all 507 frontend tests, the branding command, installer regressions and ShellCheck.
- `wails3 task package`: built `bin/Lumen.exe` and `bin/Lumen-0.10.0-x64-setup.exe`. Extracted Windows icons from both executables show the new aperture mark. This is an unsigned local build; installation on a clean machine was not repeated for this branding change.
- PNG verification checked the master, UI, favicon, Linux and Icon Composer dimensions, transparent corners and brand colours; all six ICO representations retain colour and transparency. The iOS catalogue exported all 15 PNGs with no transparent pixels.
- Native WebView2: [18 checks passed](branding-native-acceptance-2026-10-05.json), with no JavaScript errors. Onboarding, sidebar, collapsed rail, About, narrow header and navigation drawer were exercised in light and dark themes. Screenshots were inspected after transitions settled. Fixtures, data and screenshots are isolated under `<scratch>/lumen-branding-2026-10-05`; the final run used DevTools port 9431 and the test app was closed.
- Generated bindings and dependency lockfiles have no drift. macOS, Linux and mobile runtime acceptance remains unverified on this Windows machine.
- Regenerating every PNG produced identical SHA-256 hashes. The production npm dependency audit has zero findings; the existing development-tool advisory recorded in the Phase 7 review remains unchanged.

Local build SHA-256:

```text
Lumen.exe: 3C89BD1A4F19294DECE6BB66F960992F8136F71995309C058DDC4FECDDB8EF6F
Lumen-0.10.0-x64-setup.exe: E2BA8CC914F336F9AB0A0BBDDBB66E5DEA5AB98AC5D36A77B18E775692AAB9F7
```
