# Event lookup, the latency gate and thumbnail requests, 13 October 2026

Three items from the [bottleneck audit](bottleneck-audit-2026-10-13.md): P1-5
with implementation issue 1 (event work behind one mutex and inside write
transactions), issue 6 (no performance regression test) and P2-2 (thumbnail
revalidation). The first two are changed here. The third was measured and
left as it is, for the reason at the end.

## One event is found locally

`Store.Event` used to answer from `Store.Events`, which regroups every visible
photo whenever `event_revision` moved. Renaming or pinning an event, and
blocking one, made that call inside the write transaction, so the single
writer was held for a whole-library regroup and the event was read from
another snapshot than the one written to. Every grid of an event queued on
the same mutex.

Photos further apart than the gap are never one event, so the run of photos
the gap joins around a time groups exactly as the whole timeline does. The
settler already found that run (`settler.runAround`). `groupEvents` now groups
either the whole timeline or one such window, with one implementation, and
`eventIn` returns the event holding an asset from the caller's own
transaction:

- `Store.Event` uses it in one read snapshot and no longer touches the events
  cache or its mutex;
- `SetEventName`, `SetEventPinned` and `eventBlockTarget` use it on their
  write transaction.

The window is found on the base timeline (blocks ignored). Removing blocked
photos only widens gaps, so it also holds whole events of the visible
timeline. With a long gap and a library shot every day one window can be most
of the library, which is the cost a regroup had before.

`refreshEventCovers` asked one query per event each time a thumbnail batch
was saved. It is now one statement that seeks each event's newest thumbnail
through the timeline index.

`Store.Events` itself is unchanged: it still regroups the whole library when
the revision moved, under its mutex, and a caller still never gets a grouping
older than its own call. Only the Events screen and the organise preview wait
for it now.

### Figures

100,000 photos in 3,335 events, from the gate's benchmark (below), i7-13700H,
no race detector. The range is the fastest and slowest of five samples, each
the mean of three calls, in milliseconds. "Before" is the same benchmark on
the previous code, taken while other work loaded the machine, so compare the
order of magnitude, not the digits.

| Call, no blocks                           | Before  | After       |
| ----------------------------------------- | ------- | ----------- |
| `Event` after the library changed         | 165–355 | 0.33–0.46   |
| `Event`, nothing changed                  | 0.14–0.26 | 0.24–0.29 |
| `SetEventName` after the library changed  | 158–293 | 0.73–0.78   |
| `SetEventPinned` after the library changed | 284–323 | 0.61–2.18  |
| `DayItems` of an event view               | 262–307 | 0.54–0.59   |
| `DayBuckets` of an event view             | 239–348 | 0.49–0.51   |
| `Events` after a cover change             | 85–121  | 16.0–17.0   |
| `Events` after the library changed        | 178–274 | 92.5–93.8   |

The one figure that rose is an event looked up when nothing has changed: it
is resolved each time instead of read from the cached list, about a tenth of
a millisecond more.

### Tests

`internal/store/events_local_test.go`: a randomised test that, through dates
moved, photos hidden, trashed and gone missing, names, pins, event and place
blocks and every gap setting, `Event(id)` for every asset equals the member of
`Events()` that holds it; that lookups, renames, pins, blocks and event grids
complete while the events cache's mutex is held; that the lookup keeps the
snapshot that read its anchor; that a name, pin and block apply to the event
as the writer sees it; that the batched covers equal the per-event answers;
and that a window reads only its own tags.

## The latency gate

Every P1 finding of the audit was invisible at the few thousand photos the
tests and end-to-end scripts use. `scripts/bench.sh` now runs
`BenchmarkLargeLibraryStore` (formerly `BenchmarkLargeLibraryBlocking`) and
fails when a figure exceeds its ceiling in
`internal/store/testdata/library-budgets.txt`, when a call has no budget or
when a budget names no call. `scripts/lint.sh` runs it after the Go static
checks. It takes under a minute.

- The library is 100,000 photos, the product's acceptance size, seeded once.
  Dates come in two-hour bursts with quiet nights and weeks (3,335 events;
  the earlier even spread joined the library into 366), and a tenth of the
  photos share one day, the audit's P2-3 case.
- 33 calls run in three configurations (no block; a folder block; folder,
  Thing and person blocks): the reads behind each screen, the stage saves,
  ingest, event lookups and writes, duplicates, clustering data and the
  progress counts.
- A ceiling is `max(5 ms, three times the slowest of five samples)`, rounded
  up to one significant digit. It catches a regression of several times, not
  of a tenth. Ceilings are raised by hand, with a reason and five new
  samples; there is no update switch.

The checker is `scripts/benchgate`, a small Go program with its own tests.

The figures in the [blocked-assets report](blocked-assets-performance-2026-10-13.md)
were taken at 200,000 photos with the earlier dates and under the benchmark's
earlier name, so its command no longer selects anything and its figures are
not comparable with the budget file's.

What the gate does not show: the counts behind Things (about 0.3 s) and People
(0.17 s) are still recomputed on each open (P1-2 to P1-4), and the full
`Events` regroup is still a whole-library pass (93 ms).

## Thumbnail requests: measured, not changed

The audit supposed that a tile coming back into view costs a conditional
request answered "not modified", and proposed a cacheable URL carrying the
visibility revision. `e2e/perf/thumbs.mjs` measures it in the built app (3,600
photos, 224 screens, WebView2 155, 1440 × 900):

| Scenario                         | Tiles seen | Requests | Answered in full | Median request |
| -------------------------------- | ---------- | -------- | ---------------- | -------------- |
| First load                       | 18         | 30       | 30               | 118 ms         |
| Down ten screens                 | 172        | 168      | 168              | 68 ms          |
| Back up the same ten             | 168        | 133      | 133              | 12 ms          |
| Fifty-screen fling and back      | 816        | 1,438    | 1,438            | 61 ms          |
| To People and back               | 18         | 0        | 0                | n/a            |
| Ten down and up while indexing   | 303        | 384      | 384              | 51 ms          |
| Ten down and up, one folder block | 186       | 301      | 301              | 13 ms          |

- **There is never a "not modified" answer.** Wails on Windows deletes
  `If-None-Match` and `If-Modified-Since` before the request reaches Go (and
  turns a 304 into a 500), so every request is answered with the whole JPEG.
  Neither cache flag was ever set.
- **The engine is not the cost.** `MediaService.ServeHTTP` takes 0.06 to
  0.09 ms a thumbnail, with or without a block. The tens of milliseconds are
  the WebView's transport and the queue of requests behind it.
- **The privacy baseline is strict.** After a folder is blocked, the URL of a
  thumbnail fetched a moment before is refused to `fetch`, to a new `Image`
  and to cache-only fetches, and its bytes are not in the WebView's cache
  folder after shutdown. Only an image the page still holds stays readable.

A cacheable URL would let a photo cached before its block be read again
without the engine being asked, and putting `visible_value` in the URL would
change every tile's address on each ingest batch. So the header stays, and
only the comment in `services/media.go` that called the thumbnails
browser-cached is corrected. The requests are a real cost while scrolling
fast; the candidate that keeps the baseline is to keep recently shown images
alive in the grid within a memory cap, to be judged with the same script. It
was not built or measured here, and video sprites were not measured.
