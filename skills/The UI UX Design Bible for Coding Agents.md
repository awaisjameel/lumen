# The UI/UX Design Bible for Coding Agents

Oct 15, 2026 · @Awais Jameel

## How to use this guide

The prime directive: **keep what makes the product itself, remove every point of friction, and make the power visible.** Every rule below serves that sentence.

This guide is written for coding agents that build interfaces. Read it before writing UI code, re-read Part 12 before handing work back, and treat every rule as a default you may break only with a written reason.

**When rules conflict, resolve in this order:**

1. **Accessibility and safety** — never trade these for looks.
2. **The user's goal on this screen** — the content or task comes first, chrome comes last.
3. **Product identity** — the existing brand, conventions and mental models users already hold.
4. **Platform conventions** — what users expect from the OS, browser or device.
5. **Consistency with this product's design system** — tokens, components, patterns.
6. **Aesthetic preference** — trends, taste, novelty. Lowest priority, always.

**How to read the rules:** "Must" means a defect if broken. "Should" means the default; deviate only with a reason you can state in one sentence. Numbers (px, ms, ratios) are starting values, not decoration — use them unless the product's own tokens say otherwise.

## Part 1 — Core philosophy: preserve identity, eliminate friction

Juxtopposed's VLC redesign is the model case: it made a famously cluttered app feel modern without turning it into a generic player. Its six moves generalize into six laws for any software.

### Law 1 — Respect the visual heritage

The redesign kept VLC's orange traffic cone and rectangular window, softening only the corners. Recognition is an asset users already paid for; spending it to chase a trend is a loss.

- **Must** identify the product's identity anchors before redesigning: logo, signature color, window shape, key layout, vocabulary.
- **Should** evolve anchors (soften, refine, re-proportion) rather than replace them.
- **Must not** swap a distinctive shape or color for a trendy default (centered circles, purple gradients, glass everywhere) unless the user asks.

### Law 2 — Function beats fashionable layout

Trendy players center a big play button inside a tall control bar. The redesign kept play bottom-left so the controls never cover the video, and moved fullscreen, settings and tracks to the right, where people already look for them.

- **Must** keep controls off the primary content (the video, the document, the canvas).
- **Should** place controls where the user's eye and hand already go (Jakob's Law: people expect your app to work like the others they use).
- **Should** prefer a low-profile control bar over a large decorative one.

### Law 3 — Chrome appears on demand

Top and bottom bars disappear the moment the pointer leaves, so the media fills the screen. Content is the hero; UI is the servant that steps in when called.

- **Should** auto-hide non-essential chrome in immersive views (media, reading, canvas, presentation).
- **Must** bring chrome back instantly on pointer move, key press or tap — never make users hunt for it.
- **Must** keep it visible while focused, hovered or in use, and keep keyboard focus reachable.

### Law 4 — Surface the hidden superpowers

VLC's best features lived in a maze of menus. The redesign moved subtitles, audio, filters and the library into a sidebar beside the video, with live previews.

- **Must** list the product's three to five most valuable but least discovered features, and give each a visible, one-step entry point.
- **Should** let users adjust settings in context, with an immediate preview, instead of in a modal far from the result.
- **Should** replace a blank start screen with a useful home: recent items, continue where you left off, quick actions.

### Law 5 — Speak human, not machine

The old settings screen read like an airplane cockpit. The redesign grouped it into ten plain categories, swapped numeric codes for descriptive words, and gave every option an info icon.

- **Must** label settings by what they do for the user, not by the internal variable.
- **Must** give any non-obvious option a one-line explanation or info tooltip.
- **Should** group settings into roughly 7–10 top-level categories that match user goals.

### Law 6 — Customization is first-class, not an afterthought

The redesign added a drag-and-drop frame for arranging controls, saved as named profiles. Strong defaults serve most people; customization serves the rest without cluttering the default.

- **Must** ship one excellent, opinionated default layout first.
- **Should** let power users rearrange, hide and add controls, and save layouts as profiles.
- **Must** always offer "Reset to default".

**The test for every screen:** Is it still unmistakably this product? Did we remove a step, a click or a moment of confusion? Can users find the powerful thing without being told?

## Part 2 — Lessons from the masters

The best product teams agree on more than they differ: content first, restraint in color, one type family, complete interaction states, and speed as a design property. Premium is not a look; it is the visible result of many considered decisions.

### Apple — hierarchy, harmony, consistency

Apple's current Human Interface Guidelines rest on three principles: hierarchy (controls lift and distinguish the content beneath them), harmony (software echoes the shape and feel of the hardware and system) and consistency (familiar patterns behave the same everywhere). The older trio still applies too: clarity, deference (UI never competes with content) and depth (layering explains structure).

- **Rule:** the content is the hero; chrome defers to it.
- **Rule:** match the platform's corner radii, controls and gestures so the app feels native, not foreign.

### Dieter Rams — less, but better

Rams's ten principles of good design: innovative, useful, aesthetic, understandable, unobtrusive, honest, long-lasting, thorough down to the last detail, environmentally friendly, and as little design as possible.

- **Rule:** every element must earn its place. If removing it loses nothing, remove it.
- **Rule:** design for longevity over trend. A layout that will look dated in two years is a defect.

### Linear — opinionated, fast, keyboard-first

Linear treats design as product judgment, not decoration: opinionated defaults over endless options, speed as a feature, and relentless iteration on details users never consciously notice. Its interface is dense yet calm, aligned to a 4px grid, and nearly every action has a shortcut and lives in a Cmd+K command palette. Optimistic UI makes every action feel instant.

- **Rule:** choose a strong default instead of adding a setting.
- **Rule:** every action responds in the same frame; sync in the background.
- **Rule:** every frequent action gets a shortcut and a command-palette entry.
- **Caution:** copy the method, not the look. Dark canvas plus purple accent is now the most generic SaaS style there is.

### Vercel — interaction completeness and craft details

Vercel's Web Interface Guidelines and design engineer Rauno Freiberg's craft writing focus on the unglamorous details: visible focus rings, interruptible animations, never-blocked paste, inline form errors, URLs that reflect state, tabular numbers, proper ellipsis characters. The density is in behavior, not in pixels.

- **Rule:** an element is not done until it has every state: default, hover, focus, active, disabled, loading.
- **Rule:** before adding a new element, finish the states of the ones already on screen.

### Stripe — complex data made approachable

Stripe's dashboard balances information density with breathing room, anchors hierarchy in typography, uses one restrained brand color, and aligns financial figures with tabular numerals.

- **Rule:** dense data needs more whitespace discipline, not less.
- **Rule:** color carries meaning (danger, success, primary action), never decoration.

### Refactoring UI — systems over talent

Adam Wathan and Steve Schoger's practical rules: design in grayscale and add color last; build hierarchy with size, weight and color together, not size alone; use constrained scales for spacing, type, color and shadow; use spacing before borders; tint grays slightly instead of using pure neutral gray.

- **Rule:** if a screen only works because of color, its hierarchy is broken.
- **Rule:** no arbitrary values. Every dimension comes from a scale.

### Nielsen's ten usability heuristics

1. Visibility of system status — always show what is happening.
2. Match the real world — use the user's words and concepts.
3. User control and freedom — undo, cancel, back, escape.
4. Consistency and standards — same word, same action, same look.
5. Error prevention — constrain inputs, confirm destructive actions.
6. Recognition over recall — show options; don't make people remember them.
7. Flexibility and efficiency — shortcuts for experts, invisible to novices.
8. Aesthetic and minimalist design — every extra element competes with the relevant ones.
9. Help users recover from errors — plain language, the cause, the fix.
10. Help and documentation — searchable, task-focused, close to the point of need.

### Laws of UX that change code decisions

| Law | What it says | What the agent does |
| --- | --- | --- |
| Jakob's Law | Users expect your product to work like the others they use | Use conventional placement for search, settings, close, back, profile |
| Fitts's Law | Bigger, closer targets are faster to hit | Primary actions are large and near the user's focus; destructive actions are small and far |
| Hick's Law | More choices mean slower decisions | Show the 3–5 common options; tuck the rest behind "More" |
| Miller's Law / chunking | Working memory holds only a few items | Group long lists and forms into labeled chunks of 5–9 |
| Tesler's Law | Some complexity cannot be removed, only moved | Move complexity from the user into the system (smart defaults, auto-detection) |
| Doherty Threshold | Engagement drops when responses exceed about 400 ms | Respond in under 100 ms; show progress past 400 ms |
| Von Restorff Effect | The item that differs is remembered | Exactly one visually distinct primary action per view |
| Proximity / common region | Things close together or boxed together read as a group | Spacing inside a group is smaller than spacing between groups |
| Aesthetic-usability effect | Attractive design is perceived as easier to use | Polish is not optional; it buys tolerance for real friction |
| Peak-end rule | People judge an experience by its peak and its end | Invest in success states, completion moments and goodbyes |
| Goal-gradient effect | Motivation rises as the finish nears | Show progress in multi-step flows |
| Zeigarnik effect | Unfinished tasks stay in mind | Offer "continue where you left off" |

## Part 3 — Visual foundations

Everything visual comes from tokens; no raw hex, px or ms value appears in component code. Constrained scales are what make an interface look designed rather than assembled.

### Tokens first

- **Must** define color, type, spacing, radius, shadow, motion and z-index as tokens (CSS variables or theme object) before building components.
- **Must** name tokens by role, not value: `--color-text-secondary`, not `--gray-500`.
- **Must** support light and dark themes from the same role tokens.

```css
:root {
  /* surfaces */
  --bg: #fafaf9; --surface: #ffffff; --surface-raised: #ffffff; --surface-sunken: #f4f4f2;
  /* text */
  --text: #1c1c1a; --text-secondary: #5b5b57; --text-tertiary: #8a8a85;
  /* lines */
  --border: rgb(0 0 0 / 0.08); --border-strong: rgb(0 0 0 / 0.14);
  /* brand + semantic */
  --accent: #ff7a00; --accent-contrast: #1a0d00;
  --success: #1f8f4e; --warning: #b26b00; --danger: #d13b3b; --info: #2f6fdb;
  /* spacing (4px base) */
  --space-1: 4px; --space-2: 8px; --space-3: 12px; --space-4: 16px;
  --space-6: 24px; --space-8: 32px; --space-12: 48px; --space-16: 64px; --space-24: 96px;
  /* radius */
  --radius-sm: 6px; --radius-md: 10px; --radius-lg: 14px; --radius-full: 999px;
  /* motion */
  --ease-out: cubic-bezier(0.22, 1, 0.36, 1); --dur-fast: 120ms; --dur-base: 200ms; --dur-slow: 320ms;
}
```

### Color

- **60-30-10:** about 60% neutral background, 30% secondary surfaces and text, 10% or less accent.
- **One accent color.** It marks the primary action, the current selection and focus. If everything is orange, nothing is.
- **Tinted neutrals:** give grays a hint of the brand's temperature (cool blue-gray or warm stone) instead of pure neutral gray.
- **Avoid pure #000 on #FFF for large surfaces** unless chosen on purpose; near-black and off-white are easier on the eye.
- **Semantic colors** (success, warning, danger, info) are reserved for meaning. Never use red for decoration.
- **Never encode meaning in color alone** — pair it with an icon, label or shape.
- **Lower-emphasis text** uses a darker or lighter tone of the background hue, not reduced opacity over a colored surface.

**Dark mode is its own design, not an inversion:**

- Background near-black (around #0b0b0c–#141416), not #000.
- Elevation is shown by lighter surfaces, not shadows: each raised layer is a little lighter.
- Reduce saturation of accents slightly and raise their lightness so they don't vibrate.
- Body text around 85–92% white; pure white only for emphasis.
- Set `color-scheme: dark` so native scrollbars and inputs match.

### Typography

- **One family** for the UI (Inter, Geist, SF Pro, or the brand face), plus a mono for code, IDs and numbers when needed. No decorative display fonts inside product UI.
- **Type scale (px):** 12, 13, 14, 16, 18, 20, 24, 30, 36, 48, 60. Product UI lives mostly at 13–16; marketing headlines go up the scale.
- **Weights:** two or three only — 400 regular, 500 medium, 600 semibold. Bold (700+) rarely.
- **Line height:** about 1.5 for body, 1.2–1.3 for headings, 1 for single-line labels and buttons.
- **Letter spacing:** slightly negative (−0.01 to −0.02em) on large headings; slightly positive (+0.02 to +0.06em) on small uppercase labels.
- **Line length:** 60–75 characters for reading text (`max-width: 65ch`).
- **Numbers:** `font-variant-numeric: tabular-nums` in tables, timers, prices and anything that updates.
- **Hierarchy uses three levers together** — size, weight, color — not size alone.
- Use `text-wrap: balance` on headings and `text-wrap: pretty` on paragraphs to avoid orphans.

### Spacing and sizing

- **4px base grid**; most values on the 8px rhythm (8, 16, 24, 32, 48, 64).
- **Inside a group < between groups.** Related items sit close; unrelated sections get clearly more room.
- **Start generous, then tighten.** Too much whitespace is easier to fix than too little.
- **Control heights:** 28px compact, 32–36px default desktop, 40–44px comfortable or touch.
- **Padding inside a control** is roughly 1.5–2× its vertical padding horizontally (for example 8px × 14px).

### Radius

- Pick one radius family that fits the brand's personality: sharper reads technical and precise; rounder reads friendly and soft. Keep it consistent.
- **Concentric rule:** an outer container's radius = inner element's radius + the padding between them. Mismatched nested corners are a top tell of amateur UI.
- Pills (`999px`) only for tags, toggles, avatars and chips — not for every button by default.

### Elevation and depth

- Use at most three to four elevation levels: flat, raised (cards), overlay (menus, popovers), modal.
- **Shadows are layered and soft:** one tight, low-opacity shadow for the edge plus one larger, very faint one for ambient depth. Vertical offset larger than horizontal; never a hard dark single shadow.
- Prefer a 1px hairline border at low alpha, a background shift or whitespace before reaching for a shadow.
- Glass and blur effects are for surfaces that float over content (toolbars over video, sheets) — and need a solid fallback and sufficient text contrast.

### Iconography

- **One icon set**, one style, one stroke weight (1.5px or 2px at 24px). Never mix outline and filled sets randomly.
- **Sizes:** 16px inline and dense UI, 20px default, 24px prominent.
- Use a two-tone or filled variant only for a clear purpose (selected state, menu category), as the VLC redesign did for menus versus monochrome general UI.
- Optically center icons (play triangles need a nudge right); align icon and label on the text's center line.
- Icon-only buttons **must** have an accessible label and a tooltip.

## Part 4 — Layout, hierarchy and information architecture

Every screen answers three questions in under three seconds: where am I, what matters most here, and what can I do next. If a screen has two equally loud things, it has no hierarchy.

### Visual hierarchy per screen

- **Must** have exactly one primary action per view (filled accent button). Everything else is secondary (outlined or subtle) or tertiary (text or icon).
- **Must** squint-test: blur the screen; the most important element should still be the first thing you see.
- **Should** follow natural scan paths — top-left to right for dense apps (F-pattern), a clear Z for sparse marketing pages.
- **Should** separate visual hierarchy from document hierarchy: an `h1` can be visually modest if the content below is the real star.
- Labels de-emphasize, values emphasize: in key–value pairs the value is darker and heavier than the label.

### The app shell

A reliable desktop structure, from left to right: a **navigation sidebar** (collapsible, 220–280px, icons-only when collapsed at about 56–64px), the **main content** area, and an optional **inspector or context panel** on the right (280–360px) for settings that act on the current selection. A slim top bar holds title, search and global actions.

- The content area is the largest region and never sits behind chrome.
- Context panels slide in beside content, not over it, when users need to see the result while adjusting (the VLC sidebar pattern).
- Use modals only for focused, blocking decisions — not for settings people want to preview live.

### Grid and width

- 12-column grid on desktop, 4 on mobile; gutters 16–24px.
- Max content width: about 1200–1440px for apps, about 65ch for reading text. Center or left-align — never stretch prose edge-to-edge on a 27-inch monitor.
- Align everything to something. Random offsets read as mistakes.

### Responsive behavior

- Design mobile and desktop as separate layouts, not one scaled picture.
- Breakpoints by content need, roughly 640, 768, 1024, 1280px.
- Sidebars become drawers or bottom tabs on mobile; inspectors become bottom sheets.
- Touch targets grow to 44×44px on touch devices.
- Respect safe areas (`env(safe-area-inset-*)`) on notched devices.

### Information architecture

- **Navigation depth:** two levels is ideal, three the maximum before users get lost.
- **Top-level navigation:** 5–7 items. More belong in grouped sections or search.
- **Progressive disclosure:** show what 80% of users need; reveal advanced options on request ("Advanced", expandable rows, an "All settings" link).
- **Name by user goal,** not by system module: "Subtitles", not "SPU decoder".
- **The URL is state.** Tabs, filters, sort, selected item and open panels live in the URL so they can be shared, bookmarked and restored.
- **Search and a command palette** are the escape hatch for any deep IA; every destination should be reachable from them.

## Part 5 — Components and interaction patterns

A component is finished only when every state is designed and every input method works: mouse, keyboard, touch and screen reader. Default-state screenshots hide most of the quality.

### The six states (every interactive element)

| State | Requirement |
| --- | --- |
| Default | Clear affordance: it looks clickable or editable |
| Hover | Visible change within 1 frame (background shift or contrast increase), pointer devices only |
| Focus-visible | A designed ring, 2px, offset 2px, at least 3:1 contrast; never removed without a replacement |
| Active / pressed | Slight darken or `scale(0.97–0.98)` for tactile feedback |
| Disabled | Lower contrast plus `cursor: not-allowed`; say why on hover where possible |
| Loading | Keeps its size (no layout jump), shows a spinner or progress, blocks double-submit |

Also design selected, error, empty and read-only where they apply.

### Buttons

- **Hierarchy:** primary (filled accent), secondary (neutral fill or outline), tertiary (ghost or text), destructive (danger color, used only for the final confirm).
- **Labels are verbs that name the outcome:** "Save changes", "Add subtitle", not "OK" or "Submit".
- One primary per view. In dialogs the primary sits on the side the platform expects, consistently.
- Hit area at least 24×24px; extend it with padding or a pseudo-element when the visual must stay small.

### Media and canvas controls (from the VLC case)

- Controls overlay the bottom edge of the content, with a subtle gradient scrim for legibility — never a solid bar that steals height.
- Play/pause bottom-left; time next to it; fullscreen, settings, tracks and volume bottom-right.
- The seek bar is thick enough to grab (4–6px track, growing to 8px on hover) with a large invisible hit area, a hover time preview and a visible buffered range.
- Auto-hide after about 2–3 seconds of no pointer movement during playback; show instantly on any input; never hide while paused, focused or hovered.
- Keyboard: Space play/pause, arrows seek and volume, F fullscreen, M mute, C captions.

### Contextual UI and sidebars

- Settings that change what users are looking at open in a **side panel** beside the content with **live preview**, not in a modal.
- Panels are dismissible with Esc, remember their last width and tab, and never cover the thing they edit.
- Use popovers for small, local options; menus for lists of actions; a sheet or drawer for a multi-field task.

### Command palette and shortcuts

- **Cmd/Ctrl+K** opens a palette that searches actions, navigation, settings and recent items with fuzzy matching.
- Show the shortcut beside each action in menus and tooltips so people learn them passively.
- Never assign shortcuts that clash with the browser or OS; always let Esc close the top layer.

### Forms and inputs

- Labels sit above fields and are always visible; placeholders show an example, never replace a label.
- Use the right input type and `autocomplete` so mobile keyboards and password managers work.
- Validate on blur, not on every keystroke; show errors inline beside the field, and move focus to the first error on submit.
- Never block paste. Never clear the form after an error.
- Keep the submit button enabled until the request starts; then show progress.
- Group long forms into titled sections; one column is faster to complete than two.
- Warn before leaving with unsaved changes — or better, autosave.

### Lists, tables and data

- Right-align numbers, use tabular numerals, and left-align text.
- Row height 32–40px dense, 44–56px comfortable; offer density control for heavy users.
- Replace heavy grid lines with spacing or a hairline between rows.
- Sticky headers, sortable columns with a clear indicator, and virtualization above about 50 rows.
- Truncate long text with an ellipsis and a tooltip; never let one long value break the layout.

### Overlays

- **Modals:** for blocking decisions only. Trap focus, close on Esc and backdrop click (unless data would be lost), return focus to the trigger.
- **Toasts:** for confirmation and undo, bottom-center or bottom-right, 4–6 seconds, pausable on hover, announced to screen readers. Never put a critical error in a toast that disappears.
- **Tooltips:** short labels only, appear after about 300–500 ms, never hold interactive content.
- **Destructive actions:** prefer instant action with Undo over a confirmation dialog; confirm only when undo is impossible, and name the object being destroyed.

## Part 6 — Motion, feedback and performance

Motion explains change; it never decorates. Speed is the most premium feature there is: an interface that answers in under 100 ms feels like an extension of the hand.

### Response-time budget

| Delay | How it feels | What the UI does |
| --- | --- | --- |
| Under 100 ms | Instant | Respond directly; no indicator |
| 100–400 ms | Noticeable but in flow | Optimistic update, or a subtle pressed or pending state |
| 400 ms–1 s | Waiting | Skeleton or inline spinner in place of the result |
| 1–10 s | Attention drifts | Progress indicator with a label ("Scanning folder…") |
| Over 10 s | Users leave the task | Determinate progress, time estimate, cancel button, and let them keep working |

### Duration and easing

| Interaction | Duration | Easing |
| --- | --- | --- |
| Hover, press, color change | 100–150 ms | ease-out |
| Tooltips, dropdowns, popovers | 150–200 ms | ease-out |
| Panels, drawers, sidebars | 200–300 ms | ease-out or a soft spring |
| Modals and page transitions | 250–350 ms | ease-out to enter, faster ease-in to exit |
| Large or decorative moments | up to 500 ms | spring; rare |

- **Exits are faster than entrances** (about 60–75% of the enter duration).
- **Ease-out for things entering or responding** to the user; ease-in only for things leaving; linear only for progress and continuous rotation.
- **Things come from where they were triggered:** a menu grows from its button (`transform-origin` at the trigger), a panel slides from its edge.
- **Animate only `transform` and `opacity`.** Never `transition: all`; list the properties.
- **Animations are interruptible:** a new input reverses or redirects them; nothing locks the UI while it plays.
- **Stagger lists lightly** (20–40 ms per item, capped) — or not at all for frequently seen lists.
- **Respect `prefers-reduced-motion`:** replace movement with a short fade or no animation.
- **Frequent actions get the least motion.** Something used 100 times a day should feel instant, not choreographed.

### Feedback

- Every user action produces a visible response within 100 ms, even if the work takes longer.
- **Optimistic UI** for reversible, likely-to-succeed actions (toggles, renames, reorders): update immediately, reconcile in the background, roll back with a clear message on failure.
- **Skeletons** match the layout they replace; spinners only for small, inline or unknown-shape waits.
- Don't flash a loader for fast responses: delay showing it about 150–300 ms, and once shown keep it at least about 300 ms so it doesn't flicker.
- System status is always visible: saving, saved, offline, syncing, processing.

### Performance as design

- Targets (Core Web Vitals): Largest Contentful Paint under 2.5 s, Interaction to Next Paint under 200 ms, Cumulative Layout Shift under 0.1.
- Reserve space for images, media and async content (explicit `width`/`height` or `aspect-ratio`) so nothing jumps.
- Virtualize long lists; lazy-load below-the-fold media; preload critical fonts with `font-display: swap`.
- Cache aggressively and prefetch on hover or intent for likely next screens.
- Keep 60 fps: no layout reads in render loops, no heavy work on the main thread during animation.

## Part 7 — States: empty, loading, error, success, first run, home

The states most teams stub out are the ones users judge you by. Every view ships with its empty, loading, error and edge-case states designed, never as "TODO".

### Empty states

An empty state is an onboarding opportunity, not a dead end. VLC's blank cone screen became a homepage; apply the same move everywhere.

- **Must** explain what will appear here, in one sentence.
- **Must** offer the next action: a primary button ("Open a file", "Create your first project") and, where useful, a secondary ("Import", "See an example").
- **Should** support drag-and-drop when the action is adding files.
- Distinguish first-use empty ("Nothing here yet") from no-results ("No matches for 'xyz' — clear filters") from cleared ("All done").
- A small, on-brand illustration or icon is fine; a giant stock illustration is not.

### The homepage pattern

When an app opens, show where the user left off, not a blank canvas:

- **Continue:** recent files, with progress (resume at 42:10).
- **Collections:** playlists, projects, pinned items.
- **Sources:** connected folders, streams, accounts.
- **Quick actions:** open, create, import — one click each.

### Loading

- Skeletons in the shape of the real content for page and list loads.
- Keep already-loaded content visible while refreshing; show a subtle inline indicator instead of blanking the screen.
- Long jobs show determinate progress, what is happening, and a cancel.

### Errors

- **Say what happened, why, and how to fix it**, in plain words: "Couldn't load subtitles — the file isn't a supported format. Try an .srt or .ass file."
- Never blame the user; never show raw codes alone (put technical details behind "Show details").
- Keep the user's input and position; offer "Try again".
- Inline errors for fields, a banner for page-level problems, a full-page state only when the whole view failed.
- Offline is a state, not an error: show it calmly and queue actions.

### Success

- Confirm quietly for routine actions (a check, a toast with Undo).
- Celebrate rarely and briefly for real milestones — the peak-end rule says these moments shape how the whole product is remembered.

### Edge cases to test on every view

- Zero, one, few, many (10,000+) items.
- Very long names, very short names, names in other scripts (Arabic, Urdu, Chinese, emoji).
- Right-to-left layouts.
- Slow network, no network, partial failure.
- Missing images or media; permission denied; read-only access.

## Part 8 — Settings, copy and microcopy

Settings and words are the interface too. Turn the "airplane cockpit" into a short list of human categories, and write every label as if explaining it to a smart friend.

### Settings architecture

- **Categories by user goal,** roughly 7–10. A typical set: General, Appearance, Playback (or the core domain), Audio, Subtitles/Text, Controls & Shortcuts, Library & Files, Network, Privacy, Advanced.
- **Left nav of categories + a search field at the top** that matches labels, descriptions and synonyms ("captions" finds Subtitles).
- **Each setting row:** label (what), one-line description (why it matters), the control on the right, and an info icon for anything that still needs more.
- **Order within a category:** most-used first, dangerous or rare last; group related rows under small subheadings.
- **Advanced options live behind a disclosure** or in an Advanced category, never mixed into the basics.
- **Apply instantly** where safe and reversible; use explicit Save only for changes that are risky or batch-dependent.
- **Every category and the whole app offer "Reset to defaults"**, with confirmation.
- **Show changed-from-default** with a subtle dot or a "Modified" filter.
- **Previews live beside the control** — font size, subtitle color, theme, equalizer — not after closing the dialog.

### Replace machine values with human ones

| Machine-speak | Human version |
| --- | --- |
| Subtitle autodetection fuzziness: 3 | Match subtitles: Exact name only / Names that contain the video name / Any subtitle in the folder |
| Network caching (ms): 1000 | Buffer before playing: Short (faster start) / Balanced / Long (smoother on slow networks) |
| Enable hardware decoding: auto | Use graphics card for video: Automatic (recommended) |
| Error 0x80004005 | Couldn't open this file. It may be damaged or still downloading. |

Keep the exact technical value available in a tooltip or an Advanced field for experts.

### Voice and tone

- **Clear over clever.** Jokes age; clarity doesn't.
- **Active voice, second person:** "You can change this later", not "This can be changed later".
- **Front-load the key word:** "Subtitles: off", not "There are currently no subtitles enabled".
- **Specific button labels** that name the result: "Delete 3 files", "Sync subtitles".
- **Sentence case** for UI labels and buttons (or one consistent case across the product — never mixed).
- **Numbers as numerals:** "8 files", not "eight files".
- **Use the real characters:** an ellipsis (…) for in-progress ("Saving…") and for actions that open a further step; curly quotes; non-breaking spaces between a number and its unit.
- **Consistent vocabulary:** pick one word per concept (Library, not sometimes Media, sometimes Collection).
- **Localize with `Intl`** formatters for dates, times, numbers and currency; leave room for text 30–40% longer in other languages.

## Part 9 — Power users and customization

Design for two people at once: the newcomer who should never see complexity they didn't ask for, and the expert who should never be slowed down by hand-holding. Accelerators are invisible to the first and indispensable to the second.

### Layers of capability

1. **Default:** one opinionated layout that works for most people with zero setup.
2. **Discoverable shortcuts:** shown in menus and tooltips, searchable in the command palette.
3. **Context panels:** advanced options next to the content they affect, with live preview.
4. **Customization:** rearrange, hide and add controls; save named layouts.
5. **Expert escape hatch:** an Advanced settings area, import/export of configuration, and full keyboard control.

### Customization rules (the VLC drag-and-drop frame)

- **Enter an explicit "Customize" mode** — a framed, clearly different state with a palette of available controls and a Done button. Never let layouts change by accident.
- **Drag and drop with clear affordances:** grab handles, drop zones that highlight, snapping to slots, smooth reflow of neighbors.
- **Keyboard alternative** for every drag: select, then move with arrow keys or a "Move to…" menu.
- **Profiles:** save layouts by name ("Movie night", "Language study", "Editing"); switch in one click.
- **Always reversible:** undo within the session and "Reset to default" at any time.
- **Protect the essentials:** some controls (play, close, the exit from customize mode) cannot be removed.
- **Sync and export** profiles so a setup survives a reinstall or a new device.

### Keyboard-first

- Every action reachable by keyboard; frequent ones by a single key or chord.
- A "?" key or shortcut sheet lists all shortcuts, grouped by area and searchable.
- Shortcuts are remappable for anything beyond the platform standards.

### Density

- Offer Comfortable and Compact modes for data-heavy views; remember the choice per view.
- Compact mode shrinks spacing and row height, never the text below 12px or targets below 24px.

## Part 10 — Accessibility (non-negotiable)

Target WCAG 2.2 Level AA as the floor. Accessible interfaces are also the clearest, fastest interfaces for everyone, so this is quality, not compliance overhead.

| Requirement | Threshold | WCAG |
| --- | --- | --- |
| Body text contrast | at least 4.5:1 | 1.4.3 |
| Large text contrast (24px+, or 18.5px+ bold) | at least 3:1 | 1.4.3 |
| Icons, control borders, focus indicators | at least 3:1 against neighbors | 1.4.11 |
| Pointer target size | at least 24×24 CSS px, or 24px spacing | 2.5.8 |
| Focus not hidden | Focused element never fully covered by sticky bars or overlays | 2.4.11 |
| Color not the only signal | Pair color with text, icon or pattern | 1.4.1 |

### Rules for code

- **Semantic HTML first:** `<button>` for actions, `<a>` for navigation, `<label>` for inputs, real headings in order. No clickable `<div>`.
- **Every icon-only control** has an `aria-label`; decorative icons have `aria-hidden="true"`.
- **Visible focus** on everything interactive via `:focus-visible`; never `outline: none` without a replacement.
- **Full keyboard operation:** logical tab order, Esc closes overlays, focus is trapped in modals and returned afterward, a skip link to main content.
- **Live regions** (`aria-live="polite"`) announce toasts, validation and async status.
- **Never disable zoom;** layouts survive 200% zoom and 400% reflow without horizontal scroll.
- **Media:** captions and subtitles (the VLC lesson: make them easy to find and adjust), transcripts for audio, keyboard-operable player controls, no autoplay with sound.
- **Motion:** honor `prefers-reduced-motion`; nothing flashes more than three times a second.
- **Gestures** (drag, swipe, pinch) always have a click/tap and keyboard alternative.
- **Test** with a keyboard only, a screen reader (VoiceOver or NVDA), 200% zoom, forced-colors / high-contrast mode, and an automated checker (axe, Lighthouse).

## Part 11 — Anti-patterns: what makes UI look cheap or generic

Most interfaces look amateur for a small, repeatable set of reasons, and AI-generated UI repeats them most. If any item below appears in your output, fix it before handing back.

### Visual tells

- Arbitrary spacing (13px here, 17px there) instead of a scale; uneven gaps between equal items.
- Nested corners that don't follow the concentric rule.
- Too many colors, or the accent used on half the screen; gradients on everything.
- Purple-to-blue gradient hero, glowing blobs and glass cards as a default style with no brand reason.
- Hard, dark, single-layer drop shadows; borders and shadows and background color all on the same card.
- Mixed icon sets, stroke weights or icon sizes.
- More than two type families, more than three weights, or hierarchy made only from size.
- Centered body text and centered long lists.
- Pure gray text on a colored background, or low-contrast "elegant" light-gray body text.
- Emoji used as icons in a professional product.
- Everything in a card; cards inside cards inside cards.

### Interaction tells

- Missing hover, focus, pressed, disabled or loading states.
- Focus outline removed.
- Layout jumps when content, images or fonts load.
- Spinners for everything, including 50 ms operations.
- Modals for settings that should preview live; confirmation dialogs for undoable actions.
- Controls that cover the content they control.
- Hidden essentials: features only reachable through right-click or deep menus.
- `transition: all`, slow (500 ms+) animations on frequent actions, bouncy springs on serious tools.
- Disabled buttons with no hint of why.

### Content tells

- Lorem ipsum, "Item 1 / Item 2", and fake round numbers left in the final UI.
- Generic labels: OK, Submit, Click here, Learn more (with no object).
- Error messages that state a problem but no fix.
- Inconsistent capitalization and vocabulary.
- "..." instead of "…", straight quotes, numbers wrapping away from their units.

### Product tells

- Chasing a trend at the cost of identity (the opposite of keeping the cone).
- A blank start screen with no path forward.
- Settings that expose internal variables instead of user outcomes.
- Adding a new element before finishing the states of existing ones.
- Copying another company's look (Linear, Stripe, Apple) without the reasons behind it.

## Part 12 — Agent workflow and pre-ship checklist

Build in this order every time: understand, preserve, structure, style, polish, verify. Skipping straight to styling is how generic UI gets made.

### The workflow

1. **Understand the job.** Write one sentence: who uses this screen, what they are trying to do, and the single primary action. Interpret the request; don't just transcribe it.
2. **Inventory the identity.** List the product's anchors (logo, signature color, shape, layout, vocabulary, existing tokens and components). Reuse them; never silently replace them.
3. **Find the friction.** List the current pain points: steps, hidden features, confusing words, slow moments. Each design decision should remove one.
4. **Structure in grayscale.** Lay out hierarchy, grouping and spacing with neutrals only. The screen must work before any accent color is added.
5. **Apply tokens.** Color, type, radius, shadow and motion come only from the token set; add tokens rather than raw values.
6. **Complete every state.** All six interaction states, plus empty, loading, error, edge-case and responsive layouts.
7. **Surface the power.** Shortcuts, command palette entries, context panels with live preview, sensible home screen.
8. **Polish the details.** Optical alignment, concentric radii, tabular numbers, real ellipses, motion timing.
9. **Verify.** Run the checklist below, view the result at mobile and desktop widths in light and dark mode, and use it with the keyboard alone.

### Pre-ship checklist

**Identity and purpose**

- [ ] The product is still instantly recognizable (identity anchors kept)
- [ ] One clear primary action per view; passes the squint test
- [ ] Controls never cover the primary content; chrome auto-hides in immersive views
- [ ] The most valuable hidden feature is now one step away

**Visual system**

- [ ] No raw hex, px or ms values outside the token file
- [ ] Spacing on the 4/8px scale; within-group spacing smaller than between-group
- [ ] One type family, at most three weights, tabular numbers where values change
- [ ] Accent color used only for primary action, selection and focus
- [ ] Nested radii are concentric; one icon set with one stroke weight
- [ ] Dark mode designed (elevated surfaces lighter, not inverted)

**Interaction**

- [ ] Default, hover, focus-visible, active, disabled and loading states on every control
- [ ] Every action gives feedback within 100 ms; no layout shift on load
- [ ] Motion uses only transform/opacity, 100–300 ms, ease-out, interruptible, respects reduced motion
- [ ] Destructive actions offer Undo or name what will be destroyed
- [ ] Frequent actions have shortcuts and command-palette entries; Esc closes the top layer

**States and content**

- [ ] Empty, loading, error, offline and no-results states designed with a next action
- [ ] Tested with 0, 1 and 10,000 items, very long text, RTL and non-Latin scripts
- [ ] Labels are specific verbs; errors say what happened and how to fix it
- [ ] Settings are grouped by user goal, searchable, explained, and resettable

**Accessibility**

- [ ] Text contrast at least 4.5:1 (3:1 large); UI and focus contrast at least 3:1
- [ ] Targets at least 24×24px (44×44px on touch)
- [ ] Semantic elements, labels on every input and icon button, logical focus order
- [ ] Fully usable with keyboard only and with a screen reader; zoom not disabled

**Final question:** would a demanding designer at Apple, Linear or Stripe ship this screen as it stands? If not, name what they would fix, and fix it.

## Sources and further reading

The VLC redesign breakdown (Part 1) comes from the summary of Juxtopposed's video supplied with this request; timestamps there refer to the original video.

- [Vercel — Web Interface Guidelines (agent command)](https://raw.githubusercontent.com/vercel-labs/web-interface-guidelines/main/command.md)
- [Mantlr — How Stripe, Linear, and Vercel ship premium UI](https://mantlr.com/blog/stripe-linear-vercel-premium-ui)
- [Linear Method](https://linear.app/method)
- [Karri Saarinen — 10 rules for crafting products that stand out](https://www.figma.com/blog/karri-saarinens-10-rules-for-crafting-products-that-stand-out/)
- [Rauno Freiberg — Craft](https://rauno.me/craft)
- [Matt Ström-Awn — Stripe Dashboard](https://mattstromawn.com/projects/stripe-dashboard/)
- [Apple Human Interface Guidelines — overview of hierarchy, harmony, consistency](https://uithings.com/apple-human-interface-guidelines)
- [Laws of UX (Jon Yablonski)](https://lawsofux.com/index.md)
- [Refactoring UI — 7 practical tips for cheating at design](https://medium.com/refactoring-ui/7-practical-tips-for-cheating-at-design-40c736799886)
- [W3C — Understanding WCAG 2.2 Target Size (Minimum)](https://kb.daisy.org/publishing/docs/conformance/wcag/target-size-minimum.html)
- [WCAG 2.2 AA checklist for designers](https://nexasphere.io/library/wcag-2-2-aa-checklist-designers)

Dieter Rams's ten principles, Nielsen's ten heuristics, Core Web Vitals thresholds and the response-time limits are standard references and are stated from established knowledge.
