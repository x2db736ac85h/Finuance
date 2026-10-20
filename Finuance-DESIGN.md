# Finuance — Design & Architecture Document

> Finuance was previously called **CSV Inspector**. Saved data from the old name is migrated automatically (§32.1).

> **Audience:** engineers and AI coding agents who will extend or modify `finuance.html`.
> **Scope:** product intent, architecture, data model, every feature with the reasoning behind it, implementation details, invariants, extension recipes, known limitations.
> **Source of truth:** `finuance.html` (single file, ~4,100 lines, ~280 KB). Line numbers below are approximate anchors at the time of writing — always search for the quoted identifier rather than trusting a number.

---

## Table of contents

1. [Product summary](#1-product-summary)
2. [Hard constraints & design principles](#2-hard-constraints--design-principles)
3. [File anatomy (where things live)](#3-file-anatomy-where-things-live)
4. [Runtime architecture](#4-runtime-architecture)
5. [Global state model](#5-global-state-model)
6. [Data pipeline: bytes → dataset](#6-data-pipeline-bytes--dataset)
7. [Type inference & column roles](#7-type-inference--column-roles)
8. [Multi-file merge & de-duplication](#8-multi-file-merge--de-duplication)
9. [Search, filters, period, sort (`compute`)](#9-search-filters-period-sort-compute)
10. [Table view (virtualised grid)](#10-table-view-virtualised-grid)
11. [Shell UI: title bar, sidebar, toolbar, status bar, inspector](#11-shell-ui)
12. [Popover system](#12-popover-system)
13. [Insights pane & chart framework](#13-insights-pane--chart-framework)
14. [Individual insight charts](#14-individual-insight-charts)
15. [Expanded chart window (modal)](#15-expanded-chart-window-modal)
16. [Personal finance Overview](#16-personal-finance-overview)
17. [Persistence (IndexedDB), encryption, backup](#17-persistence-indexeddb-encryption-backup)
18. [Security model](#18-security-model)
19. [Theming, design tokens, chart palette](#19-theming-design-tokens-chart-palette)
20. [Interface scaling (rem system + Auto zoom compensation)](#20-interface-scaling)
21. [Responsiveness](#21-responsiveness)
22. [Events, keyboard, drag & drop](#22-events-keyboard-drag--drop)
23. [Performance characteristics](#23-performance-characteristics)
24. [Conventions & gotchas (read before editing)](#24-conventions--gotchas)
25. [Extension recipes](#25-extension-recipes)
26. [Testing approach](#26-testing-approach)
27. [Known limitations & tech-debt backlog](#27-known-limitations--tech-debt-backlog)
28. [Glossary](#28-glossary)
29. [AI assistant (local LLM chat)](#29-ai-assistant-local-llm-chat)
30. [Transaction groups & multi-selection](#30-transaction-groups--multi-selection)
31. [Per-file import settings & column mapping](#31-per-file-import-settings--column-mapping)
32. [Finuance release: rename, settings, duplicates, investments](#32-finuance-release-rename-settings-duplicates-investments)

---

## 1. Product summary

Finuance is a **single, offline HTML file** personal-finance workbench for inspecting CSV exports — primarily Polish bank statements (`;`-delimited, `dd.mm.yyyy` dates, `-127,13` comma-decimal amounts, Windows-1250 or UTF-8, `'`-prefixed account numbers) — but it works with any delimited text.

It has three surfaces:

| Surface | Purpose |
|---|---|
| **Table view** (default) | VS Code / Obsidian-style data grid: global search, per-column sort/filter, column resize/hide, row inspector, CSV export. |
| **Insights pane** (top of table view, default 50 % height) | Six interactive SVG charts scoped to the current table selection + a period picker. Each chart opens in a large modal with a table view and range settings. |
| **Overview** (separate mode) | A month-centric personal finance dashboard: KPIs with sparklines, highlights, cash flow, budgets, recurring payments, money-flow (Sankey), largest expenses, savings rate, weekday spending, running account balance. |

Data persists locally in IndexedDB (optionally encrypted with a passphrase), overlapping imports are de-duplicated, and the page is CSP-locked: it cannot make any network request except to the single AI server address the user explicitly approved (§29).

---

## 2. Hard constraints & design principles

These were explicit user requirements. **Do not violate them.**

1. **One file, fully offline.** No external scripts, stylesheets, fonts, images or CDNs. Everything is inline. A `Content-Security-Policy` meta tag enforces this (`connect-src 'none'` etc.).
2. **System fonts only** (macOS-first): `-apple-system, "SF Pro Text", …` and `ui-monospace, "SF Mono", Menlo, …` via CSS vars `--sans` / `--mono`.
3. **Look & feel:** modern, minimal, structured — VS Code / Obsidian / LM Studio. Dark default (follows OS), light theme available. Hairline borders, muted chrome, one accent (violet).
4. **Local-only data.** Nothing leaves the page. Storage is browser-profile-local IndexedDB; clearing site data must wipe everything (by design, no other storage).
5. **Robust, light, fast.** No frameworks, no build step. Vanilla JS, template-string rendering, delegated events, virtualised table.
6. **Polish-bank friendly** without being bank-specific: header detection, encoding fallback, delimiter sniffing, PL+EN header heuristics for roles.
7. **Charts follow a validated data-viz method** (see §19): fixed categorical colour order, one axis, thin marks, legends, tooltips that enhance (never gate), table twin for every chart.

---

## 3. File anatomy (where things live)

```
finuance.html
├─ <head>
│   ├─ meta charset, CSP, referrer=no-referrer, viewport
│   └─ <style>                                   (lines ~10–444)
│        :root tokens · [data-theme=dark] · [data-theme=light]
│        title bar · layout/sidebar · insights pane · grid · inspector
│        status bar · empty state · overlay/popover/toast
│        toggles/status tokens · modal · overview · lock/settings/cfg
│        @container (overview) · @media (≤1100, ≤860)
├─ <body>
│   ├─ #app.app (grid: titlebar / main / statusbar)        (~447)
│   │   ├─ header.titlebar   (#sbToggle #vizToggle #ovToggle brand(book-$ logo) #search #aiBtn #setBtn)
│   │   ├─ .main
│   │   │   ├─ aside#sidebar (Explorer #fileList, #miniDrop, Parsing opts, #sbFoot)
│   │   │   ├─ section.content
│   │   │   │   ├─ #viz            insights pane (#perSel, #vizCards, #vizRz)
│   │   │   │   ├─ .toolbar        (#viewTitle #viewMeta #chips #colsBtn #exportBtn)
│   │   │   │   ├─ #gridWrap > table#grid (colgroup#cg thead#thead tbody#tbody)
│   │   │   │   ├─ section#overview (#ovSel, #ovNote, #ovScroll > #ovGrid)
│   │   │   │   ├─ .lock > form#lockForm
│   │   │   │   └─ .empty > #bigDrop (browse / sample / demo)
│   │   │   └─ aside#inspector
│   │   └─ footer.statusbar (#stL #stR)
│   ├─ .overlay (drag & drop)   #pop (popover)   #toast   #vtip (chart tooltip)
│   ├─ #modal (expanded chart: #mTitle #mSub #mCx #mCfg #mMode #mPrev #mNext #mClose #mBody)
│   ├─ input#fileInput (multiple)   input#restoreInput (.json)
│   └─ <script> (IIFE, 'use strict')                      (~587–2598)
│        sample data · icons · helpers · state · parsing · value types
│        datasets (+roles, +dedupe) · filtering & sorting · rendering
│        popovers · insights: periods · insights: charts (CHARTS)
│        render orchestration · modal · overview (+CHARTS o_*)
│        tooltip · fields/charts menus · demo data
│        persistence (DB, CRY, prefs, boot, lock, backup, settings, cfgCtl)
│        actions (toast, copy, addFiles, setActive, removeFile, export, selection, theme)
│        wiring (DOM refs, UI scale, listeners, resize, keyboard, DnD)
│        updateToggles(); renderAll(); boot();
```

The script is one IIFE; everything is module-private. Order matters only for top-level statements that execute immediately (e.g. `applyUI(true)` runs during wiring, so anything it touches must already be defined above it — function declarations are hoisted, `const`s are not).

---

## 4. Runtime architecture

### 4.1 Layers

```
            ┌──────────────────────────── Persistence ────────────────────────────┐
            │ DB (IndexedDB: kv, files) · CRY (WebCrypto) · prefs/private/lock     │
            └──────────────▲───────────────────────────────┬───────────────────────┘
                     save/load raw bytes + prefs            │ boot()
┌──────────── Ingest ──────┴────────┐                       ▼
│ File → ArrayBuffer → hashBuf      │        S.files[] : FileEntry {buf, headers, rows, …}
│ decode → detectDelim → parseCSV   │──────▶ parseEntry(e)
│ → buildTable (header row, strip ') │
└───────────────────────────────────┘
                                   getDataset(id)  (cached in S.cache)
                                         │  per-file  or  'merged' (union + dedupe)
                                         ▼
                         Dataset {headers, rows, cols[analyzed], rowNorm, roles}
                                         │
                       compute()  (search + column filters → S.base, + period → S.order, sort)
                                         │
     ┌───────────────────────┬───────────┴───────────┬──────────────────────────┐
     ▼                       ▼                       ▼                          ▼
 renderHead/Body       renderToolbar/Status    renderViz()                renderInspector
 (virtual table)       (chips, Σ stats)        ├─ renderVizCards (CHARTS) 
                                               ├─ renderOverview (CHARTS o_*)
                                               └─ renderModal (big CHARTS + table)
```

### 4.2 Rendering philosophy

* **Immediate-mode, string templates.** Each `render*` function regenerates its region's `innerHTML` from state. No virtual DOM; regions are small or virtualised so it is cheap.
* **One orchestrator per depth:**
  * `renderAll()` — everything (used on dataset switch, mode switch, UI scale change, boot).
  * `refresh(keepScroll)` — after state changes that affect rows (search/filter/sort/period/roles). Calls `compute()` then toolbar/head/body/status/inspector/viz.
  * `renderViz()` — charts only (pane cards or overview + modal). Resets tooltip/action registries.
  * `renderBody(force)` — table rows only (virtual scroll).
* **All three end in `persist()`** (debounced) so preference changes are saved without sprinkling save calls everywhere.
* **Delegated events.** Listeners are attached once to stable containers (`thead`, `tbody`, `#chips`, `#vizCards`, `#overview`, `#mBody`, `#pop` via `pop.onclick` reassignments). Rendered markup carries `data-*` attributes that the delegates interpret.

### 4.3 Boot sequence

```
script end:  updateToggles(); renderAll();  (empty state, hidden by .booting)  boot();
boot():      DB.open() ─fail→ S.booted=true; finishBoot()  (memory-only mode)
             └ ok → applyPrefs(kv.prefs) → kv.lock?
                     ├ yes → S.locked=true → showLock() (#lockForm)  → unlock(pass) → loadSaved() → finishBoot()
                     └ no  → loadSaved() → finishBoot()
finishBoot(): remove .booting, pick active view (saved pref or 'merged' if ≥2 files), applyUI(true), setActive(id), toast
```

`S.booted` gates `persist()` so defaults rendered before boot never overwrite saved prefs.

---

## 5. Global state model

Everything lives on one object `S` (plus a few module-level `let`s for transient UI). Key fields:

| Field | Type | Meaning |
|---|---|---|
| `S.files` | `FileEntry[]` | Imported files in import order. |
| `S.active` | `string \| null` | Active view id: a file id (`f…`) or `'merged'`. |
| `S.views` | `{[viewId]: ViewState}` | Per-view UI state (see below). Lazily created by `view()`. |
| `S.cache` | `{[viewId]: Dataset}` | Dataset cache. **Clear (`S.cache = {}`) whenever files or parse options change.** |
| `S.ds` | `Dataset` | Dataset of the active view. |
| `S.base` | `number[]` | Row indices passing search + column filters (**unsorted, no period**). Used by charts that need context outside the period (cash flow, pace, recurring, overview). |
| `S.order` | `number[]` | `S.base` ∩ period, sorted. Drives the table, status totals, export, most insight charts. |
| `S.period` | `Period \| null` | Resolved period (`{kind, from, to, label}`; `to` exclusive, UTC ms). |
| `S.tokens` | `string[]` | Normalised search tokens (for highlighting). |
| `S.rowH`, `S.measured`, `S.lastKey` | | Virtual-scroll row height (measured once per scale), last rendered window key. |
| `S.opts` | `{enc, delim, strip}` | Parse options (global). |
| `S.mode` | `'table' \| 'ov'` | Table view or Overview. |
| `S.viz` | `{cards:Set, catMode}` | Visible pane cards, donut/bars toggle. |
| `S.cfg` | object | Chart range settings: `flowMonths`, `paceCmp`, `payeeTop`, `payeeScope`, `heatRange`, `ovMonths`. |
| `S.budgets` | `{[category]: number}` | User budget overrides (private). |
| `S.opening` | number | Opening account balance (private). |
| `S.tips`, `S.acts` | arrays | Per-render registries for chart tooltips and click actions (see §13.3). |
| `S.modal` | `{k, list, mode} \| null` | Expanded chart state. |
| `S.ui`, `S.uiPref` | number, `'auto' \| number` | Interface scale (§20). |
| `S.themePref` | `'system' \| 'dark' \| 'light'` | |
| `S.save`, `S.booted`, `S.locked`, `S.lock` | | Persistence flags; `S.lock` = `{salt, check}` when passphrase is set. |

**`FileEntry`**: `{id, name, size, hash, added, buf: ArrayBuffer, enc, delim, headers, rows, error, saved, eph}`
`eph` = ephemeral (sample/demo, never saved). `saved` = currently persisted in IndexedDB.

**`ViewState`** (from `view()`): `{sort:{col,dir}|null, filters:{[colName]:{cond, excluded:Set}}, search, hidden:Set, widths:{}, sel, statCol, period, custom:{from,to}|null, roles:{}, ovMonth}`
Filters/hidden/widths/sort are **keyed by column name**, not index — so they survive re-parsing and merged-view header unions.

**`Dataset`** (`makeDataset`): `{id, name, headers, rows: string[][], cols: Col[], rowNorm: string[], roles, delim, enc, dupes?}`
**`Col`** (`analyzeCol`): `{name, ci, type:'text'|'number'|'date', num:Float64Array, date:Float64Array, norm:string[], w, _rng?}` — values pre-parsed once so filtering/sorting/charts never re-parse strings.

---

## 6. Data pipeline: bytes → dataset

| Step | Function | Reasoning |
|---|---|---|
| Read | `addFiles()` → `file.arrayBuffer()` | Keep **raw bytes** so encoding/delimiter changes can re-parse without re-importing, and so persistence stores the original file losslessly. |
| Identity | `hashBuf()` SHA-256 (FNV-1a fallback) | Detect identical re-imports cheaply. |
| Decode | `decode(buf, enc)` | `auto`: strict UTF-8 (`fatal:true`), fallback **Windows-1250** (most common for Polish bank exports). Manual: UTF-8, Win-1250, ISO-8859-2, Win-1252. BOM is stripped by `TextDecoder`. |
| Delimiter | `detectDelim(text)` | For `; , \t |`, count delimiters outside quotes on the first 30 non-empty lines; score = frequency of the modal count ×1000 + modal count. Consistency beats raw count (commas inside unquoted addresses/amounts would otherwise win). |
| Tokenise | `parseCSV(text, d)` | RFC-4180-ish state machine: quoted fields, `""` escapes, CRLF/CR/LF, quotes only open at field start. |
| Shape | `buildTable(raw)` | Drop blank lines; **header = first row whose field count equals the modal field count** (skips bank preambles); trim cells; strip leading `'` (Excel text marker) if enabled; pad ragged rows; drop fully-empty unnamed columns (trailing delimiters); name blank headers `Column N`; de-duplicate header names `X (2)`. |
| Entry | `parseEntry(e)` | Sets `e.enc, e.delim, e.headers, e.rows, e.error`. |
| Dataset | `getDataset(id)` → `makeDataset()` | Analyse columns (§7), compute `rowNorm` (normalised joined row for search), detect roles. Cached in `S.cache`. |

`reparseAll()` re-runs `parseEntry` for every file (on option change) and clears caches.

---

## 7. Type inference & column roles

### 7.1 Value parsers (`parseNum`, `parseDate`, `looseDate`)

* `parseNum` accepts `-1 234,56`, `1.234,56`, `1,234.56`, `12,00 PLN`, `€12`. Rejects: >15 digits (account/reference numbers), leading-zero integers (`0123456` ids), anything with letters. Last of `,`/`.` is the decimal separator when both appear; multiple occurrences of one separator = thousands.
* `parseDate` accepts `yyyy-mm-dd[ hh:mm[:ss]]` and **day-first** `dd.mm.yyyy`/`dd/mm/yyyy`/`dd-mm-yyyy`. Returns **UTC ms** (no timezone drift; all date math in the app uses UTC helpers).
* `looseDate` (filters only) also accepts `yyyy`, `yyyy-mm`, `mm.yyyy` and returns a half-open interval `[lo, hi)`, which makes `=2025-03`, `>=2024`, `01.2025..03.2025` behave intuitively.

### 7.2 Column typing (`analyzeCol`)

A column is `date` if ≥90 % of non-empty values parse as dates, else `number` if ≥90 % parse as numbers, else `text`. Dates are checked first because `15.04.2024` would otherwise parse as a number. Default width `w` is derived from header length and the longest of the first 300 values (clamped 96–340 px, then × `S.ui`).

### 7.3 Roles (`detectRoles`, `roleCol`)

Charts and the overview need semantic columns. Roles are guessed from header names (PL + EN) and types, and can be overridden per view via **Fields** (`openFieldsMenu`, stored in `view().roles`).

| Role | Heuristic |
|---|---|
| `date` | date column matching `księgow|booking|posted|transakcji|operacji|transaction`, else first date column. |
| `amount` | number column matching `kwota|amount|warto|value|suma|…` excluding `saldo|balance`, else first non-balance number column. |
| `category` | any column matching `kategor|category`. |
| `payee` | text column matching `nadawca|odbiorca|kontrahent|payee|merchant|…`, else `opis|description|tytu|title|name|nazwa`. |
| `currency` | if a `waluta|currency` column has exactly one distinct value, it is appended to money formatting (`money()`). |
| `balance` | still detected but **unused** (balance is computed, §16.10) — vestigial. |

`roleCol(r)` resolves override → auto, and type-checks (date role must be a date column, amount must be numeric).

**Sign convention:** negative amount = spending, positive = income. Everything financial depends on this.

`payeeName(s)` normalises merchants by taking the text before the first run of 2+ spaces (bank exports pad store name and city with spaces: `ZABKA Z6373 K.1        RASZYN` → `ZABKA Z6373 K.1`).

---

## 8. Multi-file merge & de-duplication

When ≥2 files are loaded, the sidebar shows **All data**, the virtual `'merged'` view (default active view in that case).

**Union:** headers are unioned by name in first-seen order; a `Source file` column is appended (name of the file each row came from).

**De-duplication (in `getDataset('merged')`)** — needed because users import overlapping monthly/quarterly exports:

1. Build a temporary dataset to get typed columns + roles.
2. **Row key:**
   * If a reference column exists (`referenc|^ref\b|transaction id|id transakcji|numer transakcji`) and is non-empty: `r|<normalised ref>|<amount.toFixed(2)>`.
   * Else if date & amount roles exist: `<dateMs>|<amount>|<normalised payee>|<normalised title/description>`.
   * Else: the whole normalised row minus the source column.
   Keys deliberately exclude volatile fields (e.g. category, which banks may re-label between exports) and use parsed values so formatting/encoding differences don't break matching.
3. **Multiset rule:** iterate files in order, counting key occurrences *within each file*; keep a row only if its occurrence index in its own file exceeds the number already kept globally. Effect: a transaction is kept `max(count in any single file)` times. Two genuine identical purchases in one export stay as two; the same purchase appearing in two overlapping exports is kept once.
4. `ds.dupes` = rows removed, shown in the status bar; `addFiles` reports `new` vs `duplicates skipped` by diffing `uniqueRows()` before/after.

**Exact duplicate files** (same SHA-256) are rejected at import time.

Per-file views are never de-duplicated (they show the raw file).

---

## 9. Search, filters, period, sort (`compute`)

`compute()` produces `S.base` and `S.order` for the active view.

1. **Global search** (`view().search`): normalised (lowercase, NFD strip diacritics, `ł→l`), split on whitespace; **all tokens must appear** somewhere in `ds.rowNorm[i]`. Accent-insensitive search was chosen for Polish (`platnosc` finds `PŁATNOŚĆ`).
2. **Column filters** (`view().filters[colName] = {cond, excluded:Set}`):
   * `cond` compiled by `compileCond(raw, col)` into a predicate over row index:
     * `!x` negates anything. `=x` exact (normalised); `=` alone matches empty; `!=` = not empty.
     * number columns: `>`, `>=`, `<`, `<=`, `=` with locale-tolerant numbers; ranges `a..b` (inclusive).
     * date columns: same operators on `looseDate` intervals (`>2025-03` = after March), ranges, bare `2025-03`.
     * anything unparseable falls back to "contains".
   * `excluded` = set of raw values unticked in the value checklist (Excel-style).
3. → `S.base` (filters applied, no period, unsorted).
4. **Period** (`resolvePeriod()` from `view().period`): `all`, `l30`, `l3m`, `l12m`, `ytd`, `m:YYYY-MM`, `y:YYYY`, `custom` (`view().custom`). Relative presets anchor to the **latest date in the data**, not today, because statements are historical. Applied on the date role.
5. → sorted by `sortOrder()` → `S.order`. Text uses `Intl.Collator(numeric, base sensitivity)`; number/date use pre-parsed arrays; empties/NaN always last; stable via index tie-break.

Why `S.base` and `S.order` both exist: comparisons (month pace vs previous month, cash-flow context months, overview month vs history) need rows outside the selected period but still respecting the user's filters (e.g. excluding internal transfers).

---

## 10. Table view (virtualised grid)

* `<table class="grid">` with `table-layout:fixed`, `<colgroup>` widths, sticky `thead` and sticky row-number gutter (`.gutter`, original row number `i+1`).
* **Virtual scrolling** (`renderBody`): render only rows in `[scrollTop/rowH − 10, (scrollTop+height)/rowH + 10]`, with spacer rows above/below. Row height is **measured once** after the first render (`S.measured`) because rem scaling/borders make it fractional. Scroll handler is rAF-throttled and skips work if the visible window key is unchanged.
* Cells: numbers right-aligned mono tabular, negatives red (`.neg`); dates mono; search hits wrapped in `<mark>` by `hl()` (maps normalised matches back to original characters; skips highlighting when normalisation changes char length).
* Header (`renderHead`): type badge (`#` number, calendar date, `Aa` text — coloured like VS Code syntax), name, sort arrow, funnel button (visible on hover or when active), resize handle `.rz`.
  * Click header → cycle sort asc → desc → none. Right-click or funnel → column menu.
  * Drag `.rz` → resize (stored in `view().widths`), double-click → reset.
* Row click → select & open inspector; double-click a cell → copy value. Arrow keys/j/k/PageUp/PageDown move selection (`moveSel` keeps it in view).
* Empty result → single "No rows match … Clear all" row.

---

## 11. Shell UI

**Title bar:** sidebar toggle · charts pane toggle (`#vizToggle`) · overview toggle (`#ovToggle`) · brand (purple book-with-$ logo) · search (⌘F, `/`) · assistant (`#aiBtn`, ⌘J) · settings (`#setBtn`: appearance — theme & interface size —, storage, lock, backup, keyboard shortcuts). Opening files: ⌘O, the drop zones, or drag & drop anywhere. Toggles show active state via `.ib.on` (`updateToggles`).

**Sidebar (Explorer):** file list (`renderSidebar`) with All data (dedup count), per-file rows (count, `temp` tag for ephemeral, remove ×), mini drop zone, parsing options (encoding, delimiter, strip `'`), footer with storage status (`#sbFoot`: saved / encrypted / saving off / unavailable / locked). Auto-collapses below 900 px window width and restores when widened (only if it was auto-collapsed) — prevents it covering the table when the window is snapped to half screen.

**Toolbar:** view title + dimensions, **filter chips** (period, search, sort, each column condition, excluded-values count, hidden columns) each removable, *Clear all*, Columns menu, Export (filtered + sorted + visible columns, original delimiter, UTF-8 BOM for Excel).

**Status bar:** rows shown/total (accent when filtered), visible columns, delimiter, encoding (or "N files · M duplicates skipped" for merged), selected row; right side: numeric column picker + Σ / avg / min / max over `S.order`.

**Inspector:** all fields of the selected row with type badges, per-field copy, "Copy row as JSON", prev/next.

**Empty state:** big drop zone, Browse, *Load sample* (user's 7-row sample), *Load demo year* (≈900 generated fictional rows over 14 months for chart preview). Both are ephemeral.

---

## 12. Popover system

Single floating element `#pop` reused for: column menu, Columns menu, Groups, Fields, Charts (incl. order), Settings, role menu in Import settings.

* `openPop(anchor, html, width, anchorFn?)` → sets content, stores `popAnchor` (a function returning the current anchor element), calls `placePop()`.
* `placePop()` re-finds the anchor (headers are re-rendered constantly, so the original node may be detached), clamps inside the content area and viewport, flips above if needed, else pins to bottom. Called on window resize and table scroll — this is the fix for "filters must stay in place when the window is snapped to half screen".
* Per-popover handlers are assigned to `pop.onclick / oninput / onchange / onkeydown` (overwritten on each open — never `addEventListener` on `#pop`).
* `closePop()` runs `popCleanup` (column menu removes empty filter objects and re-renders head/toolbar).
* Outside-click dismissal is a global `mousedown` listener with an exclusion selector list (`[data-menu],th[data-mc],#colsBtn,#vizFields,#vizCharts,#ovFields,#setBtn,#grpBtn,[data-fields]`). **Add new popover trigger buttons to this list.**
* Width passed in is a logical px value multiplied by `S.ui`.

**Column menu** (`openColMenu`): sort asc/desc (labels depend on type), condition input with type-specific help, distinct value checklist with counts (search, All/None operate on currently shown values, capped at 600 rendered), Hide column, Clear filter.

---

## 13. Insights pane & chart framework

### 13.1 Pane

`#viz` sits above the toolbar in table mode; default height `50vh` (CSS var `--viz-h`), draggable bottom edge (`#vizRz`, 170 px … 75 vh), double-click resets. Header: period selector (`renderVizHead` builds options from data months/years; prev/next arrows for month/year/custom stepping; custom date inputs), Fields, Charts (show/hide cards), collapse. Cards are a horizontal scroll-snap row.

### 13.2 Chart contract

All charts — pane, overview and modal — are entries in one registry:

```js
CHARTS[key] = function (el, w, h, c) { … }
```

* `el` — container element (`.card-b`, overview body, or modal box). Write `el.innerHTML`.
* `w, h` — **logical** size = `el.clientWidth / S.ui`, `el.clientHeight / S.ui`. Draw SVG in logical units and open it with `svgTag(w, h)`, which sets physical width/height and a matching `viewBox` → the whole chart (text, strokes, padding) scales with the interface size. **SVG text must stay in px** (logical); see §24.
* `c` — context:
  * Table-view charts (`vizCtx()`): `{ds, dc, ac, cc, pc, per, order, base}` (`dc/ac/cc/pc` = date/amount/category/payee `Col`s).
  * Overview charts (`ovCtx()`): month aggregates (see §16.1).
  * Plus from `drawChart`: `c.sub` (header subtitle element), `c.cx` (header legend/controls element), `c.cfgEl` (modal-only range controls slot, else `null`), `c.big` (true in modal), `c.table` (set by the chart to `{cols, num:[0|1…], rows:[[…]]}` for the modal's Table view).
* `drawChart(k, el, ctx, {sub, cx, cfg, big})` wraps the call: resets slots, skips if the element is collapsed (<40 px, avoids negative SVG sizes), catches errors.

### 13.3 Tooltips & click actions (registries)

To keep markup declarative and handlers delegated:

* `tip(html)` pushes HTML into `S.tips` and returns its index → put it in `data-t="…"`.
* `act(fn)` pushes a closure into `S.acts` → `data-a="…"`.
* `bindChartRoot(root, listFn)` (attached to `#vizCards`, `#overview`, `#mBody`, `#mCfg`, `#mCx`) handles: hover → `showTip(S.tips[i])`, click → `S.acts[i]()`, `[data-expand]` → open modal, `[data-catmode]`, `[data-fields]`, `[data-ovclear]`, `[data-cfg]` selects (writes `S.cfg`, re-renders), `.bud-in` budget/opening inputs (writes `S.budgets` / `S.opening`, `savePrivate()`).
* **`renderViz()` resets both registries**, then renders pane/overview and the modal, so indices are always valid for what is on screen. Never render a chart outside `renderViz` unless you also manage the registries.
* Charts needing continuous hover (month pace crosshair) attach their own listeners to an overlay `<rect class="ov">`; `bindChartRoot` ignores `.ov`.

### 13.4 Helpers

`barPath` (bar with 3 px rounded data-end, anchored to baseline), `arc` (donut segment), `spark` (sparkline), `tipHtml(title, rows[[colour,label,value]], hint)`, `money` (2-dp + currency), `cpt` (compact axis numbers), `catColorMap` (§19), `toggleFilter(colName, cond)` (chart → table filter, toggles off if identical), `cfgCtl(c, key, label, options)` (appends a `<select data-cfg>` into `c.cfgEl` — no-op outside the modal).

---

## 14. Individual insight charts

All use `c.order` (current table selection incl. period) unless noted.

| Key | What | Implementation notes / reasoning |
|---|---|---|
| `sum` Summary | Spent, income, net (savings %), avg/day, largest expense; spent Δ vs previous equal period. | Days = period length clipped to data range (so "Last 30 days" ≠ 30 if data is shorter). Delta compares against `prevRange()` using `c.base`. Numbers shown without currency, currency in the label (fits narrow tiles). |
| `cat` Spending by category | Donut (top 7 + "Other") or bars (all). Click → table filter `=Category`. | Colours from `catColorMap` (stable by all-time spend rank, §19). Single category draws a ring (`<circle>`), not a degenerate arc. Legend always shows name, value, % (light-mode contrast relief). |
| `flow` Cash flow by month | Diverging bars: income up (`--inc`), expenses down (`--exp`); months in the selected period highlighted, others dimmed. Click month → period = that month (toggle). | Uses `c.base` (full history). **Range cfg `flowMonths`**: fit-to-width / 6 / 12 / 24 / 36 / all; capped by `pw/5` to stay legible. |
| `pace` Month pace | Cumulative spending by day of month for target month vs comparison. Crosshair tooltip. | Target = period month or latest month. **Cfg `paceCmp`**: previous month / same month last year / avg of previous 3 / 6 months / none. `cumOf(month)` builds 31-slot cumulative arrays from `c.base`. Current line stops at the last data day. |
| `payee` Top payees | Horizontal bars, one colour. Click → payee filter (contains). | **Cfg `payeeTop`** 10/25/50/100/all, **`payeeScope`** selected period (`c.order`) / all time (`c.base`). |
| `heat` Daily spending | GitHub-style calendar, sequential blue ramp, quintile bins, Mon-first weeks. Click day → custom period of that day. | **Cfg `heatRange`**: follow period / last 3, 6, 12 months / all. Cell size fits height and width; if still too wide, shows the most recent weeks; vertically centred in large view. |

---

## 15. Expanded chart window (modal)

* Opened by `[data-expand]` buttons on any card (pane or overview). `S.modal = {k, list, mode}` where `list` = the keys visible in the originating surface (for ←/→ navigation).
* `renderModal()` picks context by key prefix (`o_` → `ovCtx()`, else `vizCtx()`), draws the chart with `big:true` and `cfg:#mCfg`, then if `mode==='table'` replaces the chart with `tableHtml(c.table)`.
* Header: title, subtitle, chart legend/controls (`#mCx`), range controls (`#mCfg`), Chart/Table switch, prev/next, close. Esc closes; ←/→ navigate; backdrop click closes.
* A `ResizeObserver` re-renders on size change. Any action inside (e.g. clicking a month) goes through `refresh()/renderViz()`, which re-renders the modal too.
* **Why:** small cards are glanceable; inspection needs space, exact numbers (table twin = accessibility requirement of the data-viz method) and range control. Range choices are saved and also apply to the small card.

---

## 16. Personal finance Overview

`S.mode === 'ov'` (toggle `#ovToggle`; `.app.mode-ov` hides pane/toolbar/grid/inspector). It is month-centric (`view().ovMonth`, default latest month with data) and respects **search and column filters** (`S.base`) but **not** the table period — the header warns when filters are active ("N filters from the list applied · Clear"), and flags a month in progress.

### 16.1 Context (`ovCtx`)

Aggregates `S.base` by month: `{inc, exp, n, cats: Map, src: Map (income by payee), idx[]}` plus:
`M` (selected month), `P` (previous), `cur`, `prev`, `prevToDate` (previous month spend up to the same day — like-for-like comparison when the month is partial), `partial`, `elapsed`, `dim` (days in month), `avg3` (per-category average of up to 3 prior months), `avgExp/avgInc`, `last12` (**length from `S.cfg.ovMonths`**), `fm/lm` (first/last data month), `dmax`, `colors`, `recurring` (cached per `S.base` identity).

### 16.2 KPI row (rendered in `renderOverview`)

Income, Spending, Net saved (+savings rate), Avg/day or **Projected spend** (partial month: `spent / elapsed × dim`). Each with Δ vs previous month (direction colour depends on whether up is good) and a sparkline over `last12`.

### 16.3 Cards (CHARTS `o_*`)

| Key | Content | Reasoning / notes |
|---|---|---|
| `o_high` Highlights | Auto-generated sentences: projection, spend vs last month (same-day comparison when partial), vs 3-mo average, biggest category share, largest category increase/decrease vs usual (thresholds avoid noise), savings rate vs N-month average or overspend, no-spend days, recurring total, largest expense. | Mirrors the "insights feed" of Copilot/Monarch/Emma. Icons + text colour, never colour alone. |
| `o_flow` Cash flow | In/out bars + net dot per month over `last12`; selected month emphasised; click → select month. | Cfg `ovMonths`. Net on the same axis (same unit) — no dual axis. |
| `o_budget` Budgets | Per category: spent / budget bar, **pace marker** (expected by today = elapsed/dim), status (✓ on track, ! ahead of pace, ▲ over, ? no budget), editable budget input. Total row. | Default budget = `roundNice(3-month average)` so it works with zero setup; overrides in `S.budgets` (persisted, encrypted when locked). Status colours are reserved tokens `--st-*` with icon + label. |
| `o_recur` Recurring payments | Detected subscriptions/bills: typical amount, next expected date, yearly cost, active vs "stopped?". | `findRecurring`: per payee, ≥3 distinct months, ≤1.5 charges/month, ≥75 % of amounts within ±30 % of median, coverage ≥60 % of its month span; active if seen within 45 days of the latest data. Next = same day next month (clamped). |
| `o_sankey` Money flow | Income sources → cash → categories (+ "Saved" or "From savings" to balance). | Classic Monarch-style flow. Links coloured by source/target node at low opacity; labels truncated to fit the label column; click category → filter. |
| `o_top` Largest expenses | Top 12 (40 in modal) expenses of the month. Click → `gotoRow` (switch to table, clear incompatible period, select & scroll). | |
| `o_savings` Savings rate | Monthly rate bars (blue ≥0, red <0, clamped ±100 %) with average line. Click → select month. | Cfg `ovMonths`. |
| `o_week` Spending by weekday | Avg spend per weekday over the 3 months ending at the selected month; max day emphasised (others grey). | Emphasis form (one highlighted series) rather than 7 categorical colours. |
| `o_balance` Account balance | **Running balance = opening balance + cumulative sum of all transactions** of the dataset (filters ignored), end-of-day values over `last12`; area + line; "Opening" input in the header. | User requirement: balance must equal the sum of transactions (with opening 0 it ends at the status-bar Σ for an unfiltered all-time view). Cfg `ovMonths`. |

Layout: 12-column CSS grid inside a `container-type:inline-size` wrapper; `@container` queries collapse to 6/12 columns (≤1100 px) and full width (≤640 px). Card bodies have fixed heights (`.ov-card .card-b{flex:none;height:…}`) — **required**, otherwise grid rows collapse `flex:1` children to 0 height.

---

## 17. Persistence (IndexedDB), encryption, backup

### 17.1 Why IndexedDB

* Origin-scoped by the browser, asynchronous, stores binary (`ArrayBuffer`) natively, handles hundreds of MB, wiped by "clear site data" — exactly the requested lifecycle.
* `localStorage` was rejected: 5 MB limit, strings only, synchronous. No other storage (cookies, Cache API, OPFS) is used, so clearing site data reliably resets the app.

### 17.2 Schema (`DB`, database `finuance`, version 1 — migrated from `csv-inspector`, §32.1)

| Store | Key | Value |
|---|---|---|
| `files` | `id` (keyPath) | `{id, name, size, hash, added, data}` — `data` is the raw `ArrayBuffer`, or `{iv, ct}` when encrypted. |
| `kv` | `'prefs'` | Plain object from `collectPrefs()` (theme, ui, pane height & visibility, mode, cards, catMode, parse opts, chart cfg, save flag, active view, per-view `{roles, hidden, widths, statCol, sort, period, custom}`). Not secret, kept plain so theme/size apply before unlock. |
| `kv` | `'private'` | `{budgets, opening}` or `{enc:{iv, ct}}` when locked. |
| `kv` | `'lock'` | `{salt, check}` — exists only when a passphrase is set. |

Storing **raw files** (not parsed rows) keeps the DB compact, lossless, and lets parsing improvements/option changes apply retroactively. Parsing on load is fast (ms per thousand rows).

### 17.3 Write paths

* `persist()` — debounced 300 ms, writes `prefs`; called at the end of `renderAll`, `renderViz`, and from explicit actions (theme, pane resize/toggle, parse options). Guarded by `S.booted && !S.locked && DB.ok`.
* `saveFile(e)` — on import (non-ephemeral, non-error, when `S.save`). Also requests `navigator.storage.persist()` once (protects against eviction under storage pressure).
* `savePrivate()` — on budget/opening edits and passphrase changes.
* `removeFile` deletes the record (with confirm). `eraseAll` clears both stores and reloads.
* "Remember imported files" off → clears `files` store, marks entries unsaved, stops saving; on → saves current files.

### 17.4 Passphrase lock (`CRY`)

* PBKDF2-SHA-256, 310 000 iterations, 16-byte random salt → non-extractable AES-256-GCM key held **in memory only** (`DB.key`).
* `check` = encryption of the constant `'finuance'` (locks created under the old name used `'csv-inspector'`; both are accepted); used to verify the passphrase without decrypting files.
* Enabling/changing/removing re-encrypts everything via `resaveAll()`.
* At boot, if `kv.lock` exists the UI shows the lock screen; wrong passphrase → error; "Forgot it?" → erase all. No recovery by design.

### 17.5 Backup / restore

`exportBackup()` dumps `kv` (prefs, private, lock) and all `files` records as JSON with binary values base64-encoded (`ser`/`deser` handle `ArrayBuffer` and typed arrays). Encrypted data stays encrypted in the backup. `importBackup()` replaces the DB and reloads; the normal boot path (including unlock) then runs.

### 17.6 Failure modes

* `DB.open()` failure (private mode, browsers blocking storage for `file://`) → memory-only mode, sidebar footer explains it.
* Quota errors on save → toast "Could not save … storage may be full"; the file remains open in memory.
* Corrupted/undecryptable file records are skipped with a console error.

---

## 18. Security model

| Threat | Mitigation |
|---|---|
| Data exfiltration by the page itself (bugs, future edits, injected markup) | Static CSP meta: `default-src 'none'; connect-src http: https:; form-action 'none'; frame-src 'none'; object-src 'none'; worker-src 'none'; base-uri 'none'`, images only `data:`/`blob:`, `referrer: no-referrer`. At boot `lockNetwork()` appends a second CSP meta `connect-src <approved AI origin>` or `connect-src 'none'`; CSP policies intersect, so the page can reach **at most one origin, chosen by the user**, and nothing at all by default. Narrowing can't be relaxed at runtime, so approving a new address reloads the page. |
| Other websites reading data | Same-origin policy on IndexedDB. |
| Other **local** HTML files reading data | Chromium treats all `file://` pages as one origin. Mitigation: **optional passphrase lock** (data at rest is AES-GCM ciphertext). Firefox isolates per file path (moving the HTML changes the origin → data appears gone; use backup). Safari may block IndexedDB on `file://` (falls back to memory). |
| HTML injection from CSV content | All cell/label output goes through `esc()`; attributes use `esc()`; titles/names never interpolated raw. Keep this invariant. |
| Browser extensions with all-sites access | Cannot be prevented by a page; the lock protects data at rest only. |
| Lost passphrase | Unrecoverable by design; users are told to keep a backup. |

Not a goal: protecting against someone with an unlocked session on the machine.

---

## 19. Theming, design tokens, chart palette

* Theme = `data-theme` on `<html>` (`dark`/`light`). Initial value from `prefers-color-scheme`; explicit choice stored in `S.themePref` and persisted; OS changes are followed only while the preference is `system`.
* Tokens (dark/light blocks): surfaces `--bg --bg-2 --panel --elev --input`, borders `--border --border-2 --line`, ink `--text --text-2 --muted`, `--accent(-soft)`, `--hover(-strong) --sel`, semantic `--neg --pos --mark`, syntax-like type badge colours `--c-num --c-date --c-text`.
* **Chart tokens** (validated with a CVD/contrast validator against the app's card surfaces):
  * Categorical, fixed order: `--s1 … --s7` (blue, orange, aqua, yellow, magenta, green, violet), dark and light steps differ. Light slots 3–5 are <3:1 contrast → every categorical chart shows text labels/legend values (relief rule).
  * `--c-other` (grey for "Other"), `--c-prev` (comparison series), `--inc` / `--exp` (diverging pair blue/red), `--gridl --axis`, sequential heat ramp `--h0…--h5` (one hue, reversed in dark mode), status `--st-good --st-warn --st-crit` (fixed, always paired with icon + label).
* **Colour follows the entity, not its rank:** `catColorMap` ranks categories by **all-time** spend in the dataset and assigns slots once; filtering/period changes never repaint categories. Beyond 7 → `--c-other`. Never generate extra hues.
* Data-viz rules applied throughout: one y-axis per chart, hairline solid grid, 2 px lines, small round markers with a surface-coloured ring, 2 px surface gaps between segments, rounded data-ends, selective direct labels, tooltips enhance only, table twin in modal.

---

## 20. Interface scaling

Problem: at 100 % browser zoom the UI felt small, at 125 % too big.

* **All CSS sizes are `rem`** (converted from a px design; 1–2 px hairlines stay px). `html{font-size}` = `16px × S.ui`, so the entire chrome scales from one number.
* **JS-side sizes** that end up in pixels multiply by `S.ui`: table gutter width, default column widths, minimum column width, popover widths. Row height is measured.
* **Charts** draw in logical units and scale via `svgTag` viewBox (`w/h` passed to charts are divided by `S.ui`). Therefore SVG text uses **px** (`.ax{font:10px …}` is intentionally excluded from rem conversion) — rem inside a scaled SVG would double-scale.
* **Auto mode** (`S.uiPref='auto'`): `browserZoom()` estimates zoom from `devicePixelRatio` (base 1× or 2× display, whichever gives a zoom closest to 1) and sets `S.ui = clamp(1.12 / zoom, 0.85, 1.25)` → the app looks the same physical size at 100 % and 125 % (target ≈112 %). Re-evaluated on `resize` (zoom changes fire resize). Manual sizes 90–130 % are in Settings → Appearance. Heuristic is tuned for macOS; Windows display scaling can be misread (manual sizes are the escape hatch).
* `applyUI()` resets row measurement and re-renders everything.

---

## 21. Responsiveness

* `.app` grid uses `grid-template-columns:minmax(0,1fr)` and `minmax(0,1fr)` rows so wide tables never force page overflow (bug fixed early).
* Sidebar auto-collapse/restore at 900 px (§11). Inspector overlays content below 1100 px. ≤860 px hides button labels (`.bl`), brand name, ⌘F hint.
* Popovers re-anchor on resize and stay inside the content area.
* Status bar: left part shrinks/clips, right stats never overlap.
* Pane cards scroll horizontally; overview uses container queries.

---

## 22. Events, keyboard, drag & drop

| Input | Behaviour |
|---|---|
| ⌘/Ctrl F, `/` | Focus search. |
| ⌘/Ctrl O | File dialog. |
| Esc | Close modal → close popover → clear/blur search → deselect row. |
| ↑/↓, j/k, PgUp/PgDn | Move row selection (outside inputs). |
| ← / → | Previous/next chart in modal. |
| Drag files anywhere | Overlay (`body.dragging`, depth counter to avoid flicker), drop → `addFiles`. |
| Window resize | Sidebar auto-collapse logic, `applyUI()` (zoom), `renderBody`, `placePop`, hide tooltip. ResizeObservers re-render pane cards, overview, modal when their size changes. |

---

## 23. Performance characteristics

* Parse/analyse is O(rows × cols) once per dataset (cached). Typed arrays for numbers/dates.
* `compute()` is O(rows × (tokens + filters)) per change; search input is debounced 120 ms; filter condition 160 ms.
* Table renders ~40–80 rows at a time regardless of dataset size.
* Charts aggregate in single passes over `S.order`/`S.base`; overview recurring detection cached per `S.base` identity.
* Merged view with de-dupe builds a temporary dataset (2× analysis cost) — fine for tens of thousands of rows.
* Practical comfort zone: ≤100k rows. Memory cost is dominated by `rows` strings + `norm` arrays + `rowNorm`.

---

## 24. Conventions & gotchas

1. **Never emit unescaped data.** Use `esc()` for text and attributes.
2. **Column-keyed state.** Filters, widths, hidden, sort, roles refer to column **names**.
3. **Clear `S.cache`** whenever files, parse options or dedupe-relevant logic change; then `setActive`/`renderAll`.
4. **`S.base` vs `S.order`.** Use `order` for "what the user is looking at", `base` for history/comparisons.
5. **Dates are UTC ms.** Use `monthStart`, `addMonths`, `dayStart`, `dom`, `mKey` and `timeZone:'UTC'` formatters. Never use local-time getters.
6. **Charts:** draw in logical units, open SVG with `svgTag(w,h)`, px font sizes inside SVG, set `c.table`, put range controls via `cfgCtl` (modal only), tooltips/actions via `tip()/act()`. Don't attach listeners except on `.ov` overlays.
7. **CSS sizes in rem** (except hairlines/SVG text). New px values in CSS will not scale with the UI size setting.
8. **Popover triggers** must be added to the outside-click exclusion selector.
9. **Persistence:** new preferences → add to `collectPrefs()` and `applyPrefs()`. Secret-ish user data → `savePrivate()` payload. Never write to IndexedDB before `S.booted`.
10. **No external resources, no `localStorage`, no network** (except the approved AI origin via `fetch` in the AI module). CSP will block anything else — and silently break features. Never call `lockNetwork()` twice and never add a second narrowing meta.
11. **Overview card bodies need explicit heights** (grid + flex collapse).
12. `pop.on*` handlers are overwritten per popover — don't rely on them persisting.
13. Sign convention: expenses negative. If a bank exports positive expenses, add an "invert sign" option rather than special-casing charts.
14. `detectRoles().balance` and the `roleCol` balance branch are vestigial.

---

## 25. Extension recipes

### Add an insight chart (pane)
1. Add `['key','Title']` to `CARD_DEFS` (also registers `TITLES`).
2. Add `CHARTS.key = function (el, w, h, c) {…}` following §13.2 / §24.6 (use `c.order`/`c.base`, `c.dc/ac/cc/pc`, set `c.sub`, `c.cx`, `c.table`, optional `cfgCtl`).
3. Optional card width CSS `.card.c-key{…}`.
4. It automatically appears in the Charts menu, gets expand/modal/table support, and its visibility persists.

### Add an overview card
1. Add to `OV_DEFS`, implement `CHARTS.o_key` using `ovCtx` fields (extend `ovCtx` for new aggregates).
2. Add grid span + body height CSS: `.o_key{grid-column:span N}`, `.o_key .card-b{height:…}`; consider `@container` rules.

### Add a chart range setting
Add a default to `S.cfg`, call `cfgCtl(c, 'myKey', 'Label', [[value,'label'],…])` in the chart and read `S.cfg.myKey`. Numeric strings are coerced to numbers by the change handler. Persistence is automatic (`cfg` is in prefs).

### Add a column role
Extend `detectRoles` (regex + type), the Fields menu `roles` list in `openFieldsMenu`, and the type check in `roleCol`. Consume via `roleCol('name')`.

### Add a filter operator
Extend `compileCond` (and `HELP` text). Keep the fallback-to-contains behaviour.

### Add a persisted preference
`collectPrefs()` + `applyPrefs()` (+ reflect in UI controls). Call `persist()` after changes (or rely on `renderAll/renderViz`).

### Add a new stored entity (e.g. manual category rules, tags)
Prefer storing inside `kv` (`'private'` if sensitive — it is encrypted with the lock) unless it's large/per-row; for per-transaction annotations add a new object store (bump `indexedDB.open` version, create the store in `onupgradeneeded`, include it in `exportBackup/importBackup`, `eraseAll`, and `resaveAll` if it should be encrypted). Key annotations by the dedupe row key (§8) so they survive re-imports.

### Add a parse option
Add control in the sidebar, store in `S.opts`, read in `parseEntry/buildTable`, call `reparseAll()` + `persist()`.

### Add a settings action
Extend `openSettings()` HTML and its `pop.onclick` handler; keep destructive actions behind `confirm()`.

---

## 26. Testing approach

No test framework is embedded (single-file constraint). Development used **Playwright (Chromium, headless)** scripts against `file://…/finuance.html`:

* Load sample/demo, assert row counts, column type badges, search results, sort order, filter results, status bar text, zero `pageerror`/console errors.
* Modal: open each chart, switch Table, change `#mCfg` selects, assert subtitles.
* Overview: render, scroll, expand Sankey, simulate 100 % vs 125 % zoom (`deviceScaleFactor` 2 vs 2.5) and check `html` font size.
* Responsiveness: viewport 720 px — sidebar auto-collapse, popover stays open and inside viewport.
* Persistence: `launchPersistentContext` → import two overlapping files (different encodings/line endings) → assert dedupe toast and status; re-import identical file → skipped; reload → files/prefs restored; set passphrase → reload → wrong/right passphrase; verify `files` records contain `{iv, ct}`; demo data not persisted.
* Visual review via screenshots in dark and light themes.

Recommended for future agents: keep these scenarios as a regression checklist; add one for every new feature.

---

## 27. Known limitations & tech-debt backlog

* **Dedupe**: files with identical names share a multiset bucket; reference-less rows that legitimately differ only in category/title variants may not match; no manual "merge/unmerge" UI.
* **Recurring detection** only handles ~monthly cadence; weekly/yearly subscriptions are missed.
* **Budgets** are global by category name (not per dataset or per month); no rollover.
* **Dates** assume day-first for ambiguous `dd/mm/yyyy`.
* **Roles** overrides are per view; switching between file views and All data may require re-mapping.
* **Status bar Σ** reflects filters and period; the balance chart ignores filters (by design) — users may be confused when they differ.
* **Interface Auto scale** relies on `devicePixelRatio` heuristics.
* **`detectRoles().balance`** unused; could support reconciliation (compare computed vs bank balance).
* **CSP** uses `'unsafe-inline'` (unavoidable for a single inline file without hashing; could be tightened with a script hash if the build ever becomes reproducible).
* **No undo** for removes (confirmations only) and no transaction editing/categorisation rules.
* **Large files** (>100k rows) are memory-heavy (`rowNorm` + per-column `norm`).
* **Accessibility**: tooltips are mouse-only; keyboard focus doesn't reveal chart values (table view covers values, but focus states could be added).
* Ideas aligned with the product: category rules/re-categorisation, transfer detection (exclude own-account moves), budget periods & rollover, Sankey per period, category trend small multiples, net-worth with multiple accounts, CSV column mapping presets per bank, i18n (UI is English, data Polish).

---

## 28. Glossary

| Term | Meaning |
|---|---|
| View | A dataset being displayed: one file or `'merged'` (All data). |
| `S.base` / `S.order` | Filtered rows without / with period (order also sorted). |
| Role | Semantic meaning of a column (date, amount, category, payee). |
| Period | Table/pane time window (`view().period`). |
| `ovMonth` | Overview's selected month. |
| Ephemeral file | Sample/demo data, never persisted (`temp` tag). |
| Logical units | Chart coordinates before multiplication by `S.ui`. |
| Pace marker | Budget bar tick showing expected spend-to-date. |
| Lock | Optional passphrase encryption of persisted data. |

---

## 29. AI assistant (local LLM chat)

### 29.1 Goal & approach
Natural-language questions about the user's own transactions ("how many times did I order from Amazon last week?") answered by a **local** model (LM Studio, e.g. Qwen 2.5/3) through its OpenAI-compatible API — without sending the dataset to the model.

**Key decision: retrieval by tools, not by context dump.** The model receives only:
1. a system prompt with today's date and relative-date rules, data scope, row count, date range (latest date), currency, column schema (types, detected roles, one example value with long digit runs masked), category vocabulary with counts (≤60) and the most frequent payees (≤40);
2. three function tools executed **locally** by the app over the full data: `search_transactions`, `aggregate_transactions`, `find_values`.
The model plans the query (e.g. converts "last week" to `2026-10-12..2026-10-18`, chooses filters), the app runs it and returns only the requested rows/aggregates (rows capped by "Max rows per lookup"). Typical request size is 1.5–4k tokens regardless of dataset size; 10k rows never leave the app in bulk. This is the "evaluate the request first, then send only the needed slice" behaviour — implemented as standard tool calling.

### 29.2 Components (all in the `AI assistant` block of the script)
| Piece | Responsibility |
|---|---|
| `S.ai` (prefs) | `{base, model, mode:'auto'|'tools'|'plan', think, maxRows, temp, scope:'all'|'view', open}`. Persisted in `kv.prefs`. |
| `S.aiKey` (private) | Optional bearer token; stored in `kv.private` → encrypted when the passphrase lock is on. |
| `S.chat` | Current conversation: `id`, `created`, `turns` (UI), `history` (OpenAI messages incl. tool calls/results), `status`, `models`, `sels` (row-id sets for "Show in table", session only), `busy/ctrl` (AbortController), `noTools` (auto-fallback flag). |
| `S.chats` (kv `'chats'`) | Last 10 conversations (`MAX_CHATS`), newest first: `{id, created, updated, title, turns, history}`. Saved by `upsertCurrent()` + debounced `saveChats()` after every answer (incl. stopped/failed ones); encrypted with `encJSON` when the passphrase lock is on; included in backup, `resaveAll`, erase. Large fields are capped (tool JSON 6k chars in UI steps, 4k in history, reasoning 4k) — a typical chat is a few KB, 10 chats well under 1 MB, so loading is instant. `S.ai.keepChats=false` disables saving and deletes the key. Restored chats can be continued; their "Show in table" links are dropped because row indices change when files change. |
| `lockNetwork()` | CSP narrowing at boot (§18). `S.netOrigin` = the only allowed origin. |
| `aiData()` | Builds the tool data context: dataset = All data (merged, de-duplicated) or the current table view (`scope:'view'` uses `S.order`), roles (incl. user overrides), title & operation-type columns, date range. |
| `aiFilter`, `aiRow`, `execTool` | Local implementations of the tools. Filters: `query` (all tokens in `rowNorm`, accent-insensitive), `payee`, `category` (contains, normalised), `date_from/date_to` (inclusive ISO), `type` expense/income, `min_amount/max_amount` (absolute). Results always include matched count and totals; rows include `id` (row index) for selection. Aggregations by none/category/payee/month/week(Mon)/day/weekday/type. |
| `sysPrompt(D)`, `AI_TOOLS`, `PLAN()` | Prompt + JSON-schema tool definitions + plan-mode instructions. |
| `complete()` | Streaming `/chat/completions` (SSE parser `sse()`), assembles `content`, `reasoning_content`/`reasoning`, streamed `tool_calls`; strips `<think>` blocks into the reasoning view; parses raw Qwen `<tool_call>{…}</tool_call>` text as a fallback; also handles non-streaming JSON responses. |
| `runTools()` | Native tool loop (max 6 rounds): request → execute calls → append `tool` messages → repeat until the model answers. |
| `runPlan()` | For models without tool calling: (1) model returns `{"calls":[…]}` JSON, (2) app executes, (3) second request with results to answer. `auto` mode switches to plan mode automatically if the server rejects `tools` (HTTP 400/422/500 before any step). |
| `trimHistory()` | Last ≤30 messages starting at a user turn, older tool results truncated to 2.5k chars, plan mode strips tool messages, appends `/no_think` to the last user message when thinking is off (Qwen3 switch; harmless for other models). |
| History UI | Hovering (or clicking) the **Assistant** title opens `#aiMenu` (`renderChatMenu`): entries labelled `Interaction DD.MM HH:MM` + first question, delete ×, *New chat*. `newChat/openChat/deleteChat` first call `stopAndWait()` so an in-flight answer finishes writing to the right conversation. |
| Panel layout | `applyChatLayout()`: width from `S.ai.width` (logical px × `S.ui`, drag handle `#chatRz` on the left edge, double-click resets, max 85 % of the main area); `S.ai.max` toggles `.app.chat-max` (panel covers the whole main area, content centred at 56 rem). Both persisted in prefs. |
| `renderChat()` & friends | Right-side panel `aside#chat` (`.app.has-chat`), connection form, suggestions, message list with collapsible "Thinking", one collapsible step per lookup showing exactly what was sent to the model, "Show in table", per-answer meta (time, ~tokens sent, lookups). Minimal safe markdown renderer `md()` (escape first; paragraphs, lists, headings, code, tables). |
| `showAiSelection(k)` | Applies the full matched row-id set of a lookup as a table filter (`view().aiSel`, chip "✦ …"), switching to the right dataset and clearing period/search. |

### 29.3 Connection flow
1. Title-bar **Connect AI / Assistant** button (⌘J) opens the panel; with no address it shows the form (address default `http://localhost:1234/v1`; a bare `host:port` gets `http://` and `/v1`).
2. **Approve address & connect** validates, warns if the host is not local/private (`isPrivateHost`), saves prefs + key, then **reloads** so `lockNetwork()` can allow exactly that origin. Unsaved/demo files are mentioned in a confirm because they close on reload.
3. After reload `aiTest()` calls `GET /models`, picks a Qwen chat model (skips embedding models) unless one is saved, status dot turns green. Errors explain the usual fixes (LM Studio server running, **Enable CORS** on, address/port, API key).
4. Same-origin changes (model, mode, scope, rows, thinking, key) save without reload. **Disconnect** clears the config and reloads to return to `connect-src 'none'`.

### 29.4 Requirements on the LM Studio side
* Developer → Start server; Settings → **Enable CORS** (a `file://` page has origin `null`, so CORS must allow it).
* A chat model with tool support (Qwen2.5-Instruct, Qwen3, Llama 3.1+, Mistral…); otherwise use Plan mode.
* Context length ≥ 8k recommended (prompt + tool results + history).
* If LM Studio runs on another machine, enable "Serve on local network" and use its LAN address.

### 29.5 Extending
* **New tool:** add a schema entry to `AI_TOOLS`, implement a branch in `execTool` (return `{result, summary, sel?}`), mention when to use it in `sysPrompt` rules, add a friendly label in `stepHtml`.
* **Richer context:** add facts to `sysPrompt` only if small and stable (e.g. budgets, recurring list summary). Anything row-level belongs in a tool.
* **Persisted chats:** add a `kv` key (or store) and include it in backup/erase/resave paths; encrypt via `encJSON` when `DB.key` is set.
* **Other providers:** any OpenAI-compatible endpoint works (Ollama `/v1`, llama.cpp server, vLLM). Remote/cloud endpoints trigger the non-private-host warning by design.

### 29.6 Testing
A mock OpenAI-compatible server (Python, CORS, `/v1/models`, streaming `/v1/chat/completions` with scripted tool calls and a model that rejects `tools`) was used with Playwright to verify: requests are blocked before approval; approval reload; only the approved origin is reachable (another port is blocked by CSP); tool call streamed in fragments is assembled and executed; tool result content sent back; markdown table rendering; "Show in table" filter; automatic plan-mode fallback; no page errors.

---

## 30. Transaction groups & multi-selection

### 30.1 Purpose
User-defined, colour-coded labels for arbitrary transactions (e.g. "Italy trip 2025") so they can be filtered, totalled, exported and queried by the AI — independent of the bank's categories.

### 30.2 Data model
* `S.groups = [{id, name, color(0–7), created}]`, `S.tags = {txKey: groupId}` — one group per transaction (it is a "column").
* Saved in `kv.private` with budgets (`savePrivate`/`loadSaved`), so encrypted by the passphrase lock and included in backups.
* **Identity (`txKeys(ds)`):** same recipe as the merge de-dupe (§8): `r|ref|amount` when a reference number exists, else `date|amount|payee|title`, else the normalised row (excluding Grouping/Source); a `#n` suffix keeps identical twins in one file distinct. Keys use auto-detected roles (`ds.roles`) for stability. Labels therefore survive re-imports, overlapping exports and re-parsing.
* **Virtual column:** every dataset gets a first column `ds.gcol` (`'Grouping'`, or `'App grouping'` if the file already has one). `getDataset` prepends an empty cell to each row (copies — file rows stay untouched) and `applyGroups(ds)` fills names from `S.tags`, keeping `col.norm` and `rowNorm` in sync (so search, filters, sort, export and the value checklist all work on it). `applyGroupsAll()` re-applies to every cached dataset after any change.
* The column is hidden by `visCols()` until at least one group exists; header badge is a tag icon; cells render as coloured pills (`.gtag`).

### 30.3 UI
| Entry point | Behaviour |
|---|---|
| **Groups** toolbar button (`#grpBtn`, count badge) → `openGroupsMenu` | List with colour, name, count · spent; click = toggle filter `Grouping =Name` (reuses column filters/chips); ✎ rename/recolour (`openGroupForm`, filters referencing the old name are renamed); × delete with confirm — removes the group and its labels only, never transactions; *New group*; *Show all*. |
| **Inspector** (`renderInspector`) | "Group" row on top with a pill per group (active one outlined) + *New*. Clicking the active pill removes the label. |
| **Multi-selection** | Drag across rows (auto-scrolls near edges), Shift-click range from anchor, ⌘/Ctrl-click toggle. Stored in `view().msel` (Set of row indices), cleared by `refresh()` (filters/sort change), Esc, or plain click. Rows get `.msel`. |
| **Quick bar** (`#qbar`, `renderQbar`) | Floating at the bottom of the content area: count, Σ amount, colour dots of groups, **+** (new group then assign), *Ungroup* (if any selected row is grouped), close. Toasts move up while it is visible. |
| `openGroupForm(anchor, group|null, done)` | Shared popover: name (unique, ≤40 chars), 8-colour palette (`GCOLS` = categorical slots + `--g8`), Enter to submit. |

Click handling note: re-rendering the table on `mousedown` (modifier selection) can swallow the following `click`, so suppression after a drag uses a short time window (`suppressUntil`) and modifier clicks are ignored by the click handler instead of using a flag.

### 30.4 Integrations
* AI tools: `group` filter, `group_by:'group'`, rows include `group`, system prompt lists group names (§29).
* Charts/overview see the Grouping column as an ordinary filter target: filter to a group and the Summary card, status-bar Σ and Export reflect only that group.

---

## 31. Per-file import settings & column mapping

### 31.1 Why
Different banks export different layouts: preamble lines before the header, summary/footer lines, no header at all, other encodings/delimiters, month-first dates, debit/credit in two columns, expenses as positive numbers, and different column names. To combine several banks in **All data**, columns must be mapped to the same meanings.

### 31.2 Settings (`e.cfg`, stored with the file record in IndexedDB, part of backups)
`{enc, delim, header, skip, dropShort, dateOrder, invert, map}` — every key optional; missing keys fall back to the sidebar's general options / automatic detection (`fileOpts(e)`).

| Key | Meaning |
|---|---|
| `enc`, `delim` | Override the general encoding/delimiter for this file only. |
| `header` | 1-based record number of the header row; `0` = no header (columns `Column N`), absent = automatic. |
| `skip` | With `header:0`: first data record (1-based). |
| `dropShort` | Default on: drop rows with ≤ half the table's field count (footer lines like `Saldo końcowe;123,45`). |
| `dateOrder` | `'mdy'` converts `MM/DD/YYYY` values to ISO in mapped date columns and in any column where ≥80 % of values look like dates. Default day-first. |
| `invert` | Flip the sign of the amount (mapped Amount, debit/credit result, or an auto-guessed amount column). |
| `map` | `{columnIndex: role}` over the columns produced by `buildTable`. Roles (`MAP_ROLES`): date, vdate, amount, debit, credit, payee, desc, category, currency, ref, type, account, balance, ignore. |

### 31.3 Pipeline
`parseEntry(e)` → `fileOpts` → `decode` → `detectDelim` → `parseCSV` → **`buildTable(raw, o)`** (header row explicit/none/auto; auto picks the first row with the modal field count that is also ≥80 % filled, so padded preamble lines like `Bank export;;;` are skipped; footer drop; trims; `'` strip; empty unnamed column drop; unique names; returns `headerAt`, `dropped`) → **`applyMapping(headers, rows, o)`** (no-op without map/invert/mdy): mapped columns are renamed to canonical names (`CANON`: Date, Value date, Amount, Payee, Description, Category, Currency, Reference, Type, Account, Balance), `ignore` columns removed, debit+credit merged into one signed `Amount` (decimal comma kept when the source used it), invert applied, mdy dates converted; original columns that clash with a canonical name get ` (original)`.
`detectRoles` prefers these canonical names (`exact('Date','date')`, …), and the de-dupe/group keys already match `Reference`/`Description`, so mapped files from different banks line up in the merged view.

### 31.4 UI
* Sidebar is **hidden by default** (state remembered in prefs as `sbOpen`; auto-collapse on narrow windows still applies).
* Each file row has a ✎ button (also next to the title of a single-file view) → **Import settings** window (`#imp`, reuses `.modal` styles): settings bar; raw first 18 lines (click a line = header row; buttons *File has no header row* / *Automatic*; skipped lines struck through); preview table with the first 10 and last 5 parsed rows (a ⋯ row marks the gap); click a column name → role menu (each role once; "auto" hint shows what detection would choose); *Use detected meanings* fills the map from the hints; footer shows required fields (Date, Amount, Name, Description) and the detected encoding/delimiter/header/rows/skipped count. *Apply* stores `e.cfg`, re-parses only that file, clears caches, re-saves the record.
* The window opens automatically after an import when a file has no recognisable date or amount.
* **Source labels** `b1`, `b2`, … appear next to file names when files differ in import profile (signature = effective encoding + delimiter + parsed headers + cfg), so files from different banks are easy to tell apart.
* `impParse()` caches the raw parse per settings key; role changes only re-render. Popovers inside dialogs are clamped to the viewport and sit above modals (`.pop` z-index 55).

---

## 32. Finuance release: rename, settings, duplicates, investments

### 32.1 Rename & migration
* App name **Finuance** everywhere visible: `<title>`, brand, AI system prompt, backup files (`finuance-backup-YYYY-MM-DD.json`, key `finuanceBackup`; old `csvInspectorBackup` files still restore), toasts, this document. File renamed to `finuance.html`.
* Logo: purple gradient tile with a white **book with a dollar sign** (`ICONS.book`); the same drawing is the favicon (inline `data:image/svg+xml`, allowed by CSP `img-src data:`).
* Storage: IndexedDB renamed `csv-inspector` → `finuance`. `DB.migrate()` runs once after opening: if the new DB is empty, it opens the old one without creating it (the `onupgradeneeded` of a non-existing DB aborts), copies every `kv` entry and `files` record, then deletes the old database. Encrypted data moves as-is (the lock verifier accepts both constants).

### 32.2 Smaller UI changes
* Removed duplicated controls: title-bar *open folder* (⌘O and the drop zones remain), Explorer **+**, Overview *Operations* button (use the overview toggle), title-bar theme and interface-size buttons (moved to **Settings → Appearance**: theme System/Dark/Light, interface size select).
* Settings popover body scrolls (`.pop-scroll`). Storage hint shortened (only "✓ Protected from automatic clean-up" when persistent storage was granted).
* **Keyboard shortcuts** window (`#keysDlg`, `KEYS` table, ⌘ on Mac / Ctrl elsewhere) from Settings or the **?** key.
* **Columns** menu: one toggle button — *Hide all* when every column is visible, otherwise *Show all*.
* **Status bar totals**: the column picker appears only when the dataset has 2+ number columns ("totals of …"); default is the amount role. With one number column the Σ/avg/min/max just carry a tooltip naming the column.
* **Insights order**: Charts menu rows are draggable (HTML5 DnD, `text/plain` payload so the global file-drop overlay ignores it) and have ↑/↓ buttons. Order in `S.viz.order` (prefs `cardOrder`), read through `cardOrder()` (unknown keys dropped, new cards appended).

### 32.3 Duplicates window
* The status-bar text "N duplicates skipped" (All data view) is a link → `#dupDlg`.
* `getDataset('merged')` now keeps provenance: `ds.dup = {groups, rows (pre-dedupe), src (file index per row), keep (Uint8Array), files, headers, ri (date/amount/payee/title column indices)}`. A group = a de-dupe key present in 2+ files where something was skipped or the user chose *Keep both*.
* Dedupe rule with overrides: the **owner** file of a key is `S.dupPick[key]` (a file id) if that file contains it, else the first file; owner rows are always kept and the multiset rule applies to the other files. Keys in `S.dupKeep` are never merged. Both maps live in `kv.private` (encrypted with the lock, part of backups).
* The window lists groups (newest first, 60 at a time, text filter), each as a small table of all versions (file + key fields + every field that differs, differing cells highlighted, ✓ = in All data) with **Use this version** and **Keep both / Merge again**. Changes re-build the merged dataset (`dupChanged`).

### 32.4 Overview: Investment, Yield on cost, FI/RE
Manual data `S.inv = {ppk:{employee, employer, state, value}, positions:[{id, name, type, invested, value, income}], swr:4, ret:5}` in `kv.private` (defaults all 0). Editing only in the large view (`c.big`); inputs carry `data-inv="<id|ppk|cfg>:<field>"`, handled in `bindChartRoot`'s change handler (Enter blurs).

| Card | Content |
|---|---|
| `o_inv` **Investment** | Allocation bar (value share), PPK (*Pracownicze Plany Kapitałowe*: your contributions, employer contributions, state top-ups, current value → paid in, gain, return on your own money) and free positions (name, type, invested cost, current value, annual income; add/remove). Small card = read-only table + totals. |
| `o_yoc` **Yield on cost (PLN)** | YOC = annual dividends/interest ÷ invested cost, per position (PPK excluded — it does not pay out) plus a highlighted Total. If no annual income is entered, dividends/interest found in the transactions of the last 12 months (`dywidend|dividend|odsetk|interest|kupon|coupon`) are used and shown. |
| `o_fire` **FI/RE number** | Annual expenses (last 12 months up to the selected month, annualised if fewer months exist) × 1/SWR (25 at the default 4 %). Progress = invested value ÷ FI number (meter), years to FI by yearly compounding at the expected real return plus current yearly savings (income − expenses). SWR and real return are editable in the large view. |

Layout: the three cards share one row (3 + 3 + 6 columns; ≤1100 px → 6 + 6 + 12). Each sets `c.table` for the Table view.
