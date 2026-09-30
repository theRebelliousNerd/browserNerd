---
title: BrowserNERD Responsive Layout Audit
created: 2026-09-30
last_updated: 2026-09-30
doc_type: feature-spec
subsystem: browsernerd
read_when: Changing viewport emulation, the layout audit's detectors or CV, or chasing a mobile layout defect it missed or invented
indexes: [browsernerd-corpus, browser-automation]
---

# BrowserNERD Responsive Layout Audit

Status: shipped.

An agent reading a screenshot misses spatial defects: two badges drawn over
each other, a tab label running out of its tab, four columns squeezed side by
side on a phone, a menu whose last items sit under the phone's toolbar. This
spec makes those defects facts. A session can be sized to a phone, a tablet or
a desktop (`browser-act` `viewport`), and `browser-observe` `mode: layout`
measures the page at that size from the renderer's own geometry, confirms what
it can against the pixels, and returns each defect with the elements involved,
their boxes, and an annotated screenshot.

Code: `mcp-server/internal/browser/viewport.go` (emulation),
`mcp-server/internal/layout/` (model, detectors, CV, drawing),
`mcp-server/internal/mcp/observe_layout.go` (capture and the tool mode),
`mcp-server/internal/mcp/act_viewport.go` (the act operation).

## Viewport emulation

`{type: "viewport", ...}` in `browser-act` sizes the batch's session:

| Field | Meaning |
|---|---|
| `preset` | `phone`, `iphone-14`, `pixel-7`, `phone-small`, `tablet`, `laptop`, `desktop` |
| `width`, `height` | CSS pixels of the layout viewport; override the preset's |
| `device_scale_factor` | device pixels per CSS pixel |
| `mobile` | mobile viewport semantics (meta viewport honoured, overlay scrollbars) and a mobile user agent |
| `touch` | touch emulation: `(pointer: coarse)` and `(hover: none)` match, touch events exist |
| `orientation` | `portrait` or `landscape`; landscape swaps width and height |
| `toolbar_height` | the modeled height of the phone browser's toolbars (see below) |
| `user_agent` | an explicit user agent; default: the browser's own, rewritten to a mobile one when `mobile` |
| `reset` | back to the configured default (`browser.viewport_width` x `viewport_height`, scale 1, desktop, no touch, the browser's user agent) |

| Preset | Viewport | Scale | Mobile + touch | `toolbar_height` | Models |
|---|---|---|---|---|---|
| `phone` | 390x844 | 3 | yes | 86 | iPhone 12-16 class, Safari with its bottom toolbar |
| `iphone-14` | 390x844 | 3 | yes | 86 | iPhone 14, Safari with its bottom toolbar |
| `pixel-7` | 412x915 | 2.625 | yes | 56 | Pixel 7, Chrome with its URL bar |
| `phone-small` | 360x800 | 3 | yes | 56 | small Android, Chrome with its URL bar |
| `tablet` | 768x1024 | 2 | yes | 50 | iPad class |
| `laptop` | 1366x768 | 1 | no | 0 | |
| `desktop` | 1920x1080 | 1 | no | 0 | |

The override is CDP `Emulation.setDeviceMetricsOverride`,
`setTouchEmulationEnabled` and `setUserAgentOverride` on the session's own
target, so other agents' tabs in the shared browser are untouched. It lasts
across navigations of that tab and is recorded on the session: `browser-observe
mode: sessions` shows it per tab, and every layout audit reports the viewport
it measured. Screenshots are taken at the emulated size and scale. The
operation waits two animation frames after applying, so a page's `resize` and
`matchMedia` handlers have run, and returns the viewport it applied and the
one the page now reports (`measured`, from `Page.getLayoutMetrics`), which is
how a caller knows it took. A page with no `<meta name="viewport">` lays out
980 px wide on a mobile viewport, as it does on a real phone, and the result
carries a `note` saying so. A server restart drops the override with the
session. A batch that fails validation - an unknown preset, `reset` with
another field, a side outside 200-4096 px, a scale above 4, a
`toolbar_height` of half the height or more - runs nothing.

### Phone browser chrome: what emulation can and cannot show

On a phone, `100vh` resolves to the *large* viewport - the height with the
browser's toolbars retracted - while a page first loads with the toolbars
shown, over the bottom `lvh - svh` pixels. An element anchored to the top and
sized in `vh` (Tailwind `h-screen`), in pixels, or by its content therefore
loses its bottom strip under the toolbar; one sized with `dvh`/`svh`, a
percentage, or anchored to the bottom (`bottom: 0`, `inset-y-0`) moves with
the toolbar and stays whole. Page content that scrolls with the document is
not affected, because scrolling retracts the toolbars.

CDP cannot draw a toolbar or give `vh` and `dvh` different values, so the
preset viewport is the large viewport (as Chrome DevTools' device mode is) and
the toolbar is modeled: `toolbar_height` is the strip the audit treats as
covered for fixed elements, and the audit reads each candidate's authored
styles (`CSS.getMatchedStylesForNode`) to tell a `vh`-sized, top-anchored
drawer from a `dvh`-sized or bottom-anchored one. The toolbar heights are
approximations of the named browsers; a caller measuring a specific device
sets `toolbar_height` itself. Not modeled: the on-screen keyboard, notch and
home-indicator safe areas (`env(safe-area-inset-*)` is 0 under emulation),
and iOS Safari's own layout quirks, since the engine is Blink.

## browser-observe mode: layout

Arguments beyond `session_id` and `view`:

| Argument | Meaning |
|---|---|
| `viewports` | run the audit once per entry - a preset name or a `viewport` object - and restore the session's viewport afterwards |
| `url` | navigate the session to `url` under each viewport before auditing it, so each size gets a fresh load |
| `checks` | limit to some finding types (default all) |
| `thresholds` | `min_overlap_px` (default 3), `tap_target_px` (44), `min_column_px` (140), `collapse_below` (640), `max_findings` (200) |
| `max_items` | compact rows per viewport (default 20) |
| `full_page` | annotate a full-page capture instead of the viewport |

Without `viewports` the audit measures the session at its current size. An
unknown check or threshold name is refused with the accepted names, as
elsewhere on the surface.

A mobile page wider than the device is laid out by Chrome in a layout
viewport widened to the content and shown zoomed out. The audit then measures
against the device (the root element's client box, 390x844 on `phone`),
reports the widened width as `viewport.layout_width`, and names the cause as
`horizontal_overflow` whatever the root's `overflow-x` says, because on a
phone the page loads zoomed out either way.

### Findings

| `type` | Holds when |
|---|---|
| `overlap` | Two visible content units - text runs, badges and pills, controls, images and icons - intersect by at least `min_overlap_px` in both axes |
| `text_bleed` | A run of text extends past the box that should hold it - a tab, button, badge, cell or card - and is drawn outside it |
| `text_clipped` | Text is cut by an `overflow: hidden`/`clip` ancestor with no ellipsis and no line clamp |
| `horizontal_overflow` | The document is wider than the viewport, so the page scrolls sideways; names up to five causes, widest first: elements that stick out further than their parent does (a child merely filling an overflowing parent is not a cause), outside any container that clips or scrolls sideways, svg internals answered for by their svg |
| `offscreen` | Content past the viewport's side where the root clips horizontal overflow, so it can never be scrolled to |
| `clipped` | A control, image or badge partly cut by an ancestor that does not scroll |
| `unreachable` | Content inside a fixed element (drawer, sheet, dialog, menu, popover) that lies beyond the viewport and no scroll container can bring into view. Content cut by the fixed element's own non-scrolling clip, or by one that reaches the viewport's edge, counts as lost; content inside a smaller non-scrolling box within it (a collapsed section) is hidden on purpose |
| `toolbar_cutoff` | Content in the bottom `toolbar_height` of a top-anchored, `vh`/pixel/content-sized fixed element on a mobile viewport: hidden under the phone's toolbar |
| `non_collapsing` | Below `collapse_below` px wide, a flex row or grid still sets two or more content columns side by side and at least one is narrower than `min_column_px` with its text wrapping to three or more lines |
| `tap_target` | On a touch viewport, a control smaller than `tap_target_px` in either dimension |
| `occlusion` | Content that a fixed header, bar or floating button covers at every scroll position: under a bar at the very top of the document or at its very end. A bar hugs the top or bottom edge, is at most 35% of the viewport tall, paints above the content, and is in no overlay layer; a full-height drawer is not a bar |
| `edge_cut` | CV only: glyphs sliced by the viewport's side where no DOM finding explains it |

Severity is `high`, `medium` or `low`. `high`: text over text, controls over
controls, bleeding or offscreen text, horizontal overflow, unreachable,
toolbar-cut or permanently covered content. `medium`: other overlaps,
clipped text, clipped controls, squeezed columns, controls under 24 px.
`low`: controls between 24 px and `tap_target_px`, and overlaps whose
intersection holds no drawn content.

Each finding carries `type`, `severity`, a one-sentence `message`, the
elements involved (`selector` - `data-testid`, `id`, `aria-label`, or a short
tag/class path anchored on one - plus the BrowserNERD `ref` when the element
is a control, a text label, and its `rect`), the problem `rect` (the
intersection, the part outside, the hidden strip), the `viewport`, `in_view`
when it lies inside the captured image, its `cv` verdict, and `n`, the number
drawn on the screenshot. Findings that repeat one pattern (twelve icon
buttons of one size, a badge overlapping in every card of a grid) collapse
into one row with `count` and the members' selectors.

### What is never a finding

- An element and its own descendant: an icon inside its button, a
  notification dot inside its avatar button.
- A small absolutely positioned element over an image or icon - a status
  badge on an avatar - and images overlapping images (stacked avatars).
- Anything in an overlay layer - `role` `dialog`, `alertdialog`, `menu`,
  `listbox`, `tooltip`, a Radix popper or portal - against content outside
  that layer: a menu is meant to cover the page. Overlaps inside one layer
  still count.
- A fixed or sticky element over scrolling content, unless it covers that
  content at every scroll position (`occlusion`).
- An image below text that it is larger than: a hero or card background.
- Text truncated with `text-overflow: ellipsis`, clamped with
  `-webkit-line-clamp`, or cut inside a scroll container (it can be scrolled
  to); visually hidden text (`sr-only`, a box of 1 px or less).
- Fully clipped items in an overflow-hidden track: carousel slides.
- Inline links inside a sentence (the WCAG 2.5.8 inline exception), and
  disabled controls.
- Decorative layers: `aria-hidden` or `pointer-events: none` images and icons
  with no text.

### Pipeline

1. **Settle.** Wait two animation frames and `document.fonts.ready`.
2. **Capture geometry.** One `DOMSnapshot.captureSnapshot` of the main
   frame's document, with layout bounds, client and scroll rects, paint
   order, inline text boxes, and a fixed list of computed styles (display,
   position, visibility, opacity, overflow, z-index, text-overflow,
   white-space, line clamp, cursor, pointer events, background, borders,
   flex and grid layout), plus `Page.getLayoutMetrics` for the viewport,
   scroll offset and document size. No page script is needed for geometry,
   so the audit runs with the `js` operation gated. Iframes are not audited.
3. **Model.** Every node gets its effective visibility (display, visibility,
   opacity through its ancestors) and its clip chain: an `overflow` other
   than `visible` clips descendants to its client box, except that a
   `position: absolute` descendant escapes the clips between it and its
   containing block and a `position: fixed` one escapes all of them. Content
   is cut into units: a control is one unit (its descendants fold into it),
   so is an image, icon, input, and a small boxed element with text (a badge
   or pill); any other text is one unit per inline text box, owned by its
   element. Each unit knows its fixed or sticky root, its overlay layer, and
   its paint order.
4. **Authored styles.** On a mobile viewport with a `toolbar_height`, the
   fixed elements reaching the toolbar strip have their matched CSS rules
   read to learn how they are anchored and sized.
5. **Detect.** The detectors above run over the units with a spatial grid,
   so a page of thousands of text boxes is checked in milliseconds.
6. **Capture pixels.** One screenshot of the viewport (or the full page) at
   the emulated scale; the CV reads it at full resolution, and the annotated
   copy is shrunk to at most 800 px wide. A full-page capture is taken by
   enlarging the viewport to the document for one frame, so `vh`-sized
   elements draw taller in it than they measured; its annotation is
   illustrative and the findings still come from the true viewport.
7. **CV.** Deterministic pixel checks on the screenshot, described below,
   confirm or downgrade DOM candidates and add `edge_cut` findings.
8. **Report.** Findings are ranked (severity, then size), grouped, numbered,
   drawn on the screenshot - the first element in red, the second in blue,
   the problem region filled; a region past the screen's edge (a button
   pushed off the left side) marks the sliver of its elements still on
   screen - and saved beside other screenshots; each becomes a
   `layout_finding` fact.

### CV pass

The CV works on an edge map of the screenshot - a pixel is an edge when its
luminance differs from its right or lower neighbour by more than 40 - because
glyphs, borders and icons are edges on any background colour and flat fills
are not. It is integer arithmetic over the regions in question, so it is
deterministic and costs a few milliseconds.

- **Overlap verdict.** For an `overlap`, the edge density of the
  intersection is compared with that of each unit's own, non-overlapping
  part. Markedly more edges than either alone is `collision` - both are
  drawn, one over the other, and the finding stays at its severity. About as
  many as the top unit alone, where DOM says the lower one has text there, is
  `covered` - the upper unit hides it. Almost none is `no_ink`: the boxes
  overlap only in padding, and the finding drops to `low`.
- **Cut verdict.** For `text_bleed`, `text_clipped`, `offscreen` and
  `horizontal_overflow`, the column or row of pixels just inside the cutting
  edge is sampled over the text: edges there mean glyphs are sliced
  (`cut_visible`); for a bleed, edges outside the box mean the text is drawn
  there (`bleed_visible`).
- **Edge scan.** The viewport's left and right edges (past any classic
  scrollbar) are scanned for runs of glyph-like edges; one no DOM finding
  accounts for becomes `edge_cut`, naming the element under that point. A
  run over an image, a background image, or content in a container that
  scrolls sideways (a chip strip) is not a cut.

A cut is judged at its cutting edge, which is on screen even when the part
cut away is not. A finding whose region (or edge) is outside the captured
image is not checked and says `cv: not_in_view`. Findings the CV has no test
for (`unreachable`, `toolbar_cutoff`, `occlusion`, `non_collapsing`,
`tap_target`, `clipped`) stay `not_checked`, and compact rows omit it.

### Response

`data.viewport` (the measured size, scale, mobile, touch, toolbar height,
preset, document size and scroll), `data.counts` (by type and severity),
`data.findings` (compact rows: `n`, `type`, `severity`, `message`, the
elements' selectors and refs, `count` for a group; `view: full` adds rects,
metrics and every finding), and `data.screenshot` (`file_path`, `width`,
`height`, `annotated`). With `viewports`, `data.viewports` holds one such
block per viewport (default 10 rows each) and `data.recurring` names the
findings seen at more than one size; a viewport whose `url` failed to load
carries `navigate_error`. The `summary` line gives the size and the counts,
and `next_step` is a `browser-observe` `mode: screenshot` of the first high
finding's control, by `ref`. Evidence handle: `observe:<session>:layout`.
Budget: a three-viewport compact response stays under 3,000 tokens
(`TestLiveLayoutMultiViewport`).

## Facts

| Fact | Meaning |
|---|---|
| `viewport_emulated(SessionId, Width, Height, Scale, Mobile, Timestamp)` | A `viewport` operation applied; `Mobile` is `"true"` or `"false"` |
| `layout_finding(SessionId, FindingId, Type, Severity, Selector, ViewportWidth, Timestamp)` | One audit finding; `Selector` names its first element, `FindingId` is stable for the same defect on the same page and viewport width |

`layout_finding` is a page snapshot: the next audit regenerates it, and the
engine sheds it first.

## Verification

`mcp-server/internal/layout/` unit tests prove the geometry, the clip model
and each detector on constructed documents, and the CV verdicts on
constructed images. `mcp-server/internal/mcp/observe_layout_live_test.go`
loads the fixture pages in `mcp-server/testdata/layout/` in headless Chrome
at the `phone` preset: each defect page marks every element that must be
reported with `data-expect="<type>"`, and the test fails on any marked
element the audit misses and on any finding that names no element marked
(`data-expect` or `data-allow`) with its type; `clean.html`, built from the
intentional patterns listed above, must produce no finding. The same file
proves the viewport operation sizes only its own tab and resets, that the
phone-only checks stay quiet at `desktop`, and the multi-viewport flow and
its restore. `mcp-server/internal/mcp/observe_layout_test.go` covers the
authored-style cascade and the operation's validation.
`mcp-server/internal/mcp/observe_layout_smoke_live_test.go` audits a running
app on demand: pointed at a signed-in shared browser
(`BROWSERNERD_LAYOUT_SMOKE_DEBUGGER`, `_BASE`, `_OUT`, optional `_ROUTES` with
`path@selector` to scroll a spot into view, `_CHECKS`, `_PRESET`), it opens one
tab of its own, records what the page reports about the device
(`innerWidth`, `devicePixelRatio`, `(pointer: coarse)`, a mobile user agent)
and a plain screenshot, audits each route, writes the report, and closes only
its tab.

| Fixture | Defects it holds |
|---|---|
| `overlap.html` | two "Proprietary" badges on each other, a label over a headline, two buttons stacked |
| `bleed.html` | four-column tab labels bleeding into the next tab, a fixed-width button label, a name cut with no ellipsis, a line cut in half by a fixed height |
| `overflow.html` | a 600 px banner widening the page |
| `offscreen.html` | a row past the edge of a page whose root hides horizontal overflow |
| `fixed.html` | a `100dvh` menu that does not scroll (unreachable), a `100vh` scrolling menu (toolbar cut-off) |
| `columns.html` | three text columns squeezed side by side |
| `taps.html` | a 20 px button, a bare checkbox, a 26 px-tall link |
| `occlusion.html` | a title under a fixed header, a last button under a fixed footer |
| `clean.html` | none: every intentional pattern above |
