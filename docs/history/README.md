# Development history

Dated records of how Lumen was designed, built and checked, phase by phase. They are historical: each describes the code on its date, and later records correct earlier ones. Where a record and the code differ, the code and [the README](../../README.md) win. Paths such as `<scratch>` name the scratch folders measurements were made in; they were never part of the repository.

The living documents are one folder up: [the feature tour](../features.md), [the beta guide](../beta-testing.md), [production signing](../production-signing.md), [branding](../branding.md), [GeoNames data](../geonames.md) and [the original plan](../plan.md).

## Phases

| Phase | Records (the newest applies) |
| --- | --- |
| 5: organisation | [blocking](blocking-phase5-2026-09-30.md), [design of the remainder](phase5-design-2026-10-01.md) |
| 6: polish and ship v1 | [design](phase6-design-2026-10-03.md), [performance pass](performance-2026-10-03.md), [packaged beta 0.11.0 acceptance](beta-package-acceptance-2026-10-10.md) |
| 7: Nuxt UI frontend | [baseline inventory](phase7-inventory-2026-10-04.md), [RAW and rendered-image feasibility](phase7-media-feasibility-2026-10-04.md), [updater feasibility](phase7-updater-feasibility-2026-10-04.md), [design and record](phase7-design-2026-10-04.md), [implementation review](phase7-review-2026-10-04.md) |
| 8: viewing and file tools | [implementation](phase8-implementation-2026-10-05.md), audits of [5 October](phase8-audit-2026-10-05.md) and [6 October](phase8-audit-2026-10-06.md) |
| 9: camera RAW and formats | [record](phase9-formats-2026-10-07.md) |
| 10: customisation | [record](phase10-customisation-2026-10-08.md), [audit](phase10-audit-2026-10-09.md) |
| 11: lossless compression | [record](phase11-compression-2026-10-09.md), [audit](phase11-audit-2026-10-10.md) (corrects the record where they differ) |
| 12: application updates | [trust and activation foundations](phase12a-updater-2026-10-10.md), [implementation](phase12-implementation-2026-10-12.md) |
| Interface redesign | [record](redesign-2026-10-19.md) |

Phases 0 to 4 have no records of their own: Phase 0's measurements are in [the spikes](../../spikes/README.md), and Phases 1 to 4 are covered by the audits below.

## Plans

- [Frontend and media development plan](next-development-plan-2026-10-04.md): the scope, architecture and acceptance gates for Phases 7 to 12.
- [Automatic update plan](automatic-updates-plan-2026-10-04.md): the updater's limited network exception and recovery design.
- [Lossless compression plan](lossless-compression-plan-2026-10-04.md).

## Audits

- 28 September 2026: [implementation audit](audit-2026-09-28.md), then [the current-state audit](audit-2026-09-28-current.md)
- 29 September 2026: [end-to-end audit](audit-2026-09-29.md), [benchmark and performance audit](benchmark-2026-09-29.md)
- 30 September 2026: [staged Places and Events audit](audit-2026-09-30.md)
- 1 October 2026: [end-to-end audit](audit-2026-10-01.md) and [the hardening pass that followed it](audit-report-2026-10-01.md), which introduced [the conventions](../../CONVENTIONS.md)
- 3 October 2026: [branch review](audit-2026-10-03-branch.md)
- 13 October 2026: [bottleneck and implementation audit](bottleneck-audit-2026-10-13.md), [materialised block coverage](blocked-assets-performance-2026-10-13.md), [event lookup and the latency gate](event-lookup-latency-gate-2026-10-13.md)
- 14 October 2026: [the audit's P2 items](audit-p2-round-2026-10-14.md), [kept counts](kept-counts-2026-10-14.md), [name order](name-order-2026-10-14.md)
- 15 October 2026: [SQLite tuning](sqlite-tuning-2026-10-15.md)

## Acceptance logs

The JSON files are the raw reports of native WebView2 acceptance runs, each cited by the record beside it: [branding](branding-native-acceptance-2026-10-05.json) ([branding guide](../branding.md)); Phase 8 [implementation](phase8-native-acceptance-2026-10-05.json), audit runs of [5 October](phase8-audit-native-acceptance-2026-10-05.json) and [6 October](phase8-audit-native-acceptance-2026-10-06.json), the [6 October tour](phase8-audit-native-tour-2026-10-06.json) and [dependency audit](phase8-audit-dependencies-2026-10-06.json); [Phase 9](phase9-native-acceptance-2026-10-07.json); Phase 10 [record](phase10-native-acceptance-2026-10-08.json) and [audit](phase10-audit-native-2026-10-09.json).
