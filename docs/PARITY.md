# Legacy parity checklist

Every capability of the legacy single-file app (`legacy/finuance.html`, Finuance release), with the phase that ports it.

- **Statuses:** `todo` · `ported` · `retired: reason`.
- **Legacy refs** point at `legacy/DESIGN.md` sections (§n) or legacy functions.
- **Rows marked 🧪** are covered by the legacy oracle (`testdata/oracle/`).
- **Phase 14** (task 14.6) requires every row to be `ported` or `retired`.

## 1. Ingest and import

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Keep raw bytes; re-parse on option change | §6 `addFiles`, `reparseAll` | 6 | todo |
| Identical-file rejection (SHA-256) | §6 `hashBuf`, §8 | 6 | todo |
| Encoding auto: strict UTF-8, else Windows-1250; BOM stripped 🧪 | §6 `decode` | 3 | todo |
| Manual encodings UTF-8 / Windows-1250 / ISO-8859-2 / Windows-1252 🧪 | §6, `ENC_LABEL` | 3 | todo |
| Delimiter detection `; , \t \|` by modal count consistency 🧪 | §6 `detectDelim` | 3 | todo |
| RFC-4180-ish tokenizer: quotes, `""`, CRLF/CR/LF, quotes only at field start 🧪 | §6 `parseCSV` | 3 | todo |
| Table shaping: blank lines, auto header (modal count + 80 % fill), trim, `'` strip, ragged rows, empty unnamed columns, `Column N`, `X (2)` 🧪 | §6, §31.3 `buildTable` | 3 | todo |
| Footer/summary row drop (`dropShort`) 🧪 | §31.2 | 3 | todo |
| Explicit header row / no header + skip 🧪 | §31.2 `header`, `skip` | 3, 9 | todo |
| Month-first dates (`dateOrder: 'mdy'`) 🧪 | §31.2 `applyMapping` | 3, 9 | todo |
| Invert sign (`invert`), incl. amount-column guess 🧪 | §31.2 `applyMapping` | 3, 9 | todo |
| Column mapping to canonical roles (date, vdate, amount, debit, credit, payee, desc, category, currency, ref, type, account, balance, ignore) 🧪 | §31.2 `MAP_ROLES`, `CANON` | 3, 6, 9 | todo |
| Debit + credit → one signed amount; kept as plain columns when a signed amount is mapped 🧪 | §31.2 `applyMapping` | 3 | todo |
| Value parsers `parseNum`, `parseDate`, `looseDate` 🧪 | §7.1 | 3 | todo |
| Column typing (≥ 90 % date, else ≥ 90 % number, else text) 🧪 | §7.2 `analyzeCol` | 3 | todo |
| Role detection (date, amount, category, payee, currency, balance) 🧪 | §7.3 `detectRoles` | 3 | todo |
| `norm`, `payeeName`, `collapse` 🧪 | §4.1 of PLAN, §7.3 | 3 | todo |
| Import settings window: every control (encoding, delimiter, header row from raw lines, no header / automatic, skip, dropShort, dateOrder, invert, role per column, preview first 10 + last 5) | §31.4 `#imp` | 9 | todo |
| Import window opens automatically when no date/amount is recognised | §31.4, `addFiles` | 9 | todo |
| Source labels `b1`, `b2` for differing import profiles | §31.4 | 9 | todo |
| Import toast: "N new operations · M duplicates skipped" | `addFiles` | 9 | todo |
| Remembered per-layout settings (import profiles, fast path) | PLAN §1.3 | 6, 9 | todo (new) |

## 2. De-duplication and duplicates window

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Dedupe key: `ref + amount`, else `date + amount + payee + title`, else normalised row 🧪 | §8 `getDataset('merged')` | 4, 6 | todo |
| Multiset rule: keep max(count in any single file) 🧪 | §8 | 6 | todo |
| *All data* merged view with `Source file` column | §8 | 7 | retired: replaced by *All accounts*; dedupe is per account (PLAN §1.3 [CHANGE]); provenance is the import history |
| "N duplicates skipped" status link → Duplicates window | §32.3 | 10 | todo |
| Duplicates window: groups newest first, 60 per page + *Show more*, text filter, versions side by side, differing cells highlighted, ✓ kept marker 🧪 | §32.3 `renderDups` | 10 | todo |
| *Use this version* (`dupPick`) 🧪 | §32.3 | 6, 10 | todo |
| *Keep both* / *Merge again* (`dupKeep`) 🧪 | §32.3 | 6, 10 | todo |

## 3. Table view

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Virtualised grid, sticky header and row-number gutter, measured row height | §10 `renderBody` | 10 | todo |
| Cell rendering: numbers right-aligned mono, negatives red, dates mono, search highlight `<mark>` | §10 `hl()` | 10 | todo |
| Header: type badge, sort arrow, funnel, resize handle (drag; double-click resets) | §10 `renderHead` | 10 | todo |
| Sort cycle asc → desc → off; collator numeric/base; empties last; stable | §9 `sortOrder` | 7, 10 | todo |
| Row click → inspector; double-click cell → copy; ↑/↓, j/k, PgUp/PgDn | §10, §22 | 10 | todo |
| Empty result row "No rows match … Clear all" | §10 | 10 | todo |
| Global search: all tokens, accent-insensitive (`platnosc` → `PŁATNOŚĆ`) 🧪 | §9 | 7, 10 | todo |
| Column filter language: `!x`, `=x`, `=`, `!=`, `> >= < <= =` for numbers and dates, `a..b`, `looseDate` intervals, contains fallback 🧪 | §9 `compileCond` | 7 | todo |
| Value checklist (Excel-style): counts, search, All/None on shown values, cap 600 | §12 `openColMenu` | 10 | todo |
| Column menu: sort, condition with type-specific help, checklist, hide, clear | §12 | 10 | todo |
| Columns menu: show/hide each column; single *Hide all* / *Show all* toggle | §32.2 `openColsMenu` | 10 | todo |
| Toolbar chips (period, search, sort, each condition, excluded count, hidden columns), each removable, *Clear all* | §11 `renderToolbar` | 10 | todo |
| Export CSV: filtered + sorted + visible columns, original delimiter, UTF-8 BOM | §11 | 7, 10 | todo |
| Status bar: rows shown/total, columns, delimiter, encoding, selected row | §11 `renderStatus` | 10 | todo |
| Status bar Σ / avg / min / max; column picker only with 2+ number columns, else a tooltip names the column | §11, §32.2 | 10 | todo |
| Inspector: all fields with type badges, per-field copy, *Copy row as JSON*, prev/next | §11 `renderInspector` | 10 | todo |
| Fields menu (role overrides per view) | §7.3 `openFieldsMenu` | — | retired: roles are mapped per import in the preview (PLAN §7.6) |

## 4. Groups and multi-selection

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Groups `{id, name, color 0–7}`; one group per transaction; identity survives re-import 🧪 | §30.2 `txKeys` | 4, 10 | todo |
| Virtual Grouping column (`Grouping` / `App grouping`), hidden until a group exists, coloured pills | §30.2 | 10 | todo |
| Groups menu: colour, name, count · spent; click toggles `Grouping =Name` filter | §30.3 `openGroupsMenu` | 10 | todo |
| Rename / recolour (filters referencing the old name are renamed); delete removes labels only | §30.3 `openGroupForm` | 10 | todo |
| Group form: unique name ≤ 40 chars, 8-colour palette, Enter submits | §30.3 | 10 | todo |
| Inspector group row: pill per group, active outlined, click active removes, + New | §30.3 | 10 | todo |
| Multi-selection: drag across rows (auto-scroll), Shift-click range, ⌘/Ctrl-click toggle; cleared by filter/sort change, Esc, plain click | §30.3 | 10 | todo |
| Quick bar: count, Σ amount, group dots (assign), + (new and assign), Ungroup, close; toasts move up | §30.3 `renderQbar` | 10 | todo |
| Groups in assistant tools, charts and export | §30.4 | 10, 13 | todo |

## 5. Insights pane and charts

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Pane above the toolbar, default 50 vh, resizable 170 px … 75 vh, double-click reset, collapse | §13.1 | 11 | todo |
| Period picker: all, l30, l3m, l12m, ytd, `m:YYYY-MM`, `y:YYYY`, custom; anchored to latest data date; prev/next stepping 🧪 | §9 `resolvePeriod`, §13.1 | 7, 11 | todo |
| Charts menu: show/hide cards and **reorder** (drag + ↑/↓), persisted `cardOrder` | §32.2 `cardOrder` | 11 | todo |
| `sum` Summary: spent, income, net (savings %), avg/day, largest expense, Δ vs previous period | §14 | 11 | todo |
| `cat` Spending by category: donut (top 7 + Other) / bars; click → `=Category`; single-category ring | §14 | 11 | todo |
| `flow` Cash flow by month; range `flowMonths` (fit / 6 / 12 / 24 / 36 / all) | §14 | 11 | todo |
| `pace` Month pace with crosshair; range `paceCmp` (prev month / same month last year / avg 3 / avg 6 / none) | §14 | 11 | todo |
| `payee` Top payees; ranges `payeeTop` (10/25/50/100/all), `payeeScope` (period / all time); click → payee filter | §14 | 11 | todo |
| `heat` Daily spending calendar; range `heatRange` (follow / 3 / 6 / 12 months / all); click day → custom period | §14 | 11 | todo |
| Expanded chart window: Chart/Table switch, range controls, prev/next, Esc / ← / →, backdrop click | §15 `renderModal` | 11 | todo |
| Table twin for every chart (`c.table`) 🧪 (investment cards) | §13.2 | 11, 12 | todo |
| Tooltips (`tip`) and click actions (`act`), chart → table filter toggle | §13.3 | 11 | todo |
| Category colours stable by all-time rank (`catColorMap`); > 7 → Other | §19 | 11 | todo |

## 6. Overview

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Month-centric overview: respects search + column filters but not the table period; "N filters applied · Clear"; month-in-progress flag | §16 | 12 | todo |
| KPI row: income, spending, net saved + savings rate, avg/day or projected spend; Δ vs previous month; sparklines | §16.2 | 12 | todo |
| `o_high` Highlights | §16.3 | 12 | todo |
| `o_flow` Cash flow (range `ovMonths`) | §16.3 | 12 | todo |
| `o_budget` Budgets: default `roundNice(3-month avg)` 🧪, pace marker, status icons, editable input, total row | §16.3 | 12 | todo |
| `o_recur` Recurring payments (`findRecurring`) 🧪 | §16.3 | 12 | todo |
| `o_sankey` Money flow | §16.3 | 12 | todo |
| `o_top` Largest expenses (12, 40 in modal); click → go to row | §16.3 | 12 | todo |
| `o_savings` Savings rate (range `ovMonths`) | §16.3 | 12 | todo |
| `o_week` Spending by weekday | §16.3 | 12 | todo |
| `o_balance` Account balance = opening + cumulative sum; "Opening" input (range `ovMonths`) | §16.3 | 12 | todo |
| `o_inv` Investment: allocation bar, PPK (employee, employer, state, value), positions (add/remove); editing in large view 🧪 | §32.4 | 12 | todo |
| `o_yoc` Yield on cost (PLN), with dividend/interest fallback from transactions 🧪 | §32.4 | 12 | todo |
| `o_fire` FI/RE number: annual expenses / SWR, progress, years to FI; SWR and real return editable 🧪 | §32.4 | 12 | todo |
| Layout: 12-column grid, container queries (6/12 ≤ 1100 px, full ≤ 640 px); investment row 3 + 3 + 6 | §16.3, §32.4 | 12 | todo |

## 7. AI assistant

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Tools `search_transactions`, `aggregate_transactions`, `find_values`: every filter, sort, `group_by` 🧪 | §29.2 `execTool`, `aiFilter` | 13 | todo |
| System prompt: today, relative-date rules, scope, schema, categories ≤ 60, payees ≤ 40, groups 🧪 | §29.2 `sysPrompt` | 13 | todo |
| Streaming completions; `reasoning_content` / `<think>`; Qwen `<tool_call>` text fallback | §29.2 `complete` | 13 | todo |
| Native tool loop (max 6 rounds) | §29.2 `runTools` | 13 | todo |
| Plan mode for models without tools; auto fallback on HTTP 400/422/500 | §29.2 `runPlan` | 13 | todo |
| History trimming (30 messages, 2.5k tool results, `/no_think`) | §29.2 `trimHistory` | 13 | todo |
| Chat history: last 10 chats, menu with *Interaction DD.MM HH:MM*, delete, New chat | §29.2 `renderChatMenu` | 13 | todo |
| Panel: resizable width, maximise, persisted | §29.2 `applyChatLayout` | 13 | todo |
| Steps (collapsible, exactly what was sent), *Show in table* chip "✦ …", per-answer meta | §29.2 `showAiSelection` | 13 | todo |
| Suggestions; minimal safe markdown `md()` | §29.2 | 13 | todo |
| Settings: model, mode (auto/tools/plan), thinking, max rows, temperature, scope (all/view) | §29.2 `S.ai` | 13 | todo |
| ⌘J toggles the panel | §22 | 13 | todo |
| Connection flow: browser approves an LLM address, reload narrows the CSP to that origin | §29.3 `lockNetwork` | — | retired: the server orchestrates an admin-configured endpoint; browser CSP stays `connect-src 'self'` (PLAN §1.3, ADR 0019) |
| API key in browser storage (`S.aiKey`) | §29.2 | — | retired: `FINUANCE_AI_API_KEY` server secret |

## 8. Shell, appearance, keyboard

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| Title bar: sidebar, charts pane and overview toggles, brand, search (⌘F, `/`), assistant, settings | §11 | 8 | todo |
| Purple book-with-$ logo and favicon | §32.1 | 8 | todo |
| Sidebar (Explorer) hidden by default (`sbOpen`), auto-collapse < 900 px and restore | §11, §31.4 | 8 | todo |
| Sidebar file list with counts, remove ×, mini drop zone | §11 | 8, 9 | todo (becomes accounts + import history) |
| Sidebar parsing options (encoding, delimiter, strip `'`) | §11 | 9 | todo (become per-import options) |
| Settings → Appearance: theme System/Dark/Light, interface size incl. Auto | §32.2 | 8 | todo |
| Dark/light design tokens, chart palette, status tokens | §19 | 8 | todo |
| Interface scaling: rem-based, `S.ui`, Auto zoom compensation | §20 | 8 | todo |
| Responsiveness: inspector overlay < 1100 px, labels hidden ≤ 860 px | §21 | 8, 10 | todo |
| Popovers that re-anchor on resize/scroll; outside-click/Esc close | §12 `placePop` | 8 | todo |
| Toasts | §11 | 8 | todo |
| Drag & drop anywhere with overlay (files only) | §22 | 9 | todo |
| ⌘/Ctrl O file dialog | §22 | 9 | todo |
| Keyboard shortcuts window (`?`, from Settings; ⌘ on Mac / Ctrl elsewhere) | §32.2 `KEYS` | 8 | todo |
| Esc order: modal → popover → search → selection | §22 | 8, 10 | todo |
| Empty state: drop zone, Browse | §11 | 9 | todo |
| *Load demo year* (fictional, ~14 months) 🧪 (`demo-legacy.csv`) | §11 `demoCSV` | 9, 18 | todo |
| *Load sample* (7-row sample statement) | §11 `SAMPLE` | — | retired: the demo year covers previews; the scrubbed sample becomes a fixture |

## 9. Persistence, security, backup

| Feature | Legacy ref | New phase | Status |
|---|---|---|---|
| IndexedDB persistence (files, prefs, private, chats) | §17 | — | retired: PostgreSQL + server-side preferences (Phase 5) |
| "Remember imported files" toggle; ephemeral demo files | §17.3 | — | retired: imports are always stored; demo data is a normal import that can be deleted |
| Preferences persisted (theme, ui, pane, cards, chart cfg, per-view sort/widths/hidden/period) | §17.2 `collectPrefs` | 5, 8 | todo |
| Passphrase lock (PBKDF2 310k → AES-256-GCM), lock screen | §17.4 | — | retired: replaced by server auth (PLAN §7.6) |
| Backup download / restore | §17.5 | 14 | todo (export via `GET /me/export`; restore of new exports in backlog) |
| **Legacy backup migration**, plain and encrypted (browser-side decryption) 🧪 (`legacy-backup/`) | §17.5, PLAN §4.10 | 14 | todo |
| Erase all | §17.3 | 5 | todo (delete account / data) |
| Strict CSP, all output escaped | §18 | 8 | todo (no `innerHTML`, `connect-src 'self'`) |
| Rename migration `csv-inspector` → `finuance` | §32.1 | — | retired: one-time browser storage migration; the backup importer accepts `csvInspectorBackup` |

## 10. Known MVP failures (reported by Irwin)

| Failure | Cause (from the oracle) | Fix in phase |
|---|---|---|
| **mBank:** header is not on line 1, so columns aren't recognised | Auto header picks the first row with the modal field count. The header `#Data operacji;…;#Kwota;` has 6 fields, but data rows end in `;;` and have 7, so the header is never a candidate. When the ~25-line preamble outnumbers the data rows, the bank's address line becomes the header (`ingest/synthetic/banks/mbank-like.json`, `headerAt: 1`). Otherwise the first transaction becomes the header (`ingest/real/mbank/mbank_example.json`, `headerAt: 27`). | 3 (header detection), 9: **fixed** — header found at record 26 (`ALLOWED_DIFFS.md` row 4) |
| **PKO BP:** column names aren't picked up correctly | The header has 6 named + 5 blank columns, and description fields spill into the blank columns. The 80 %-fill rule rejects the header, and the first data row becomes the header (`ingest/synthetic/banks/pko-bp-like.json` and `ingest/real/pko-bp/pko_bp_example.json`, `headerAt: 2`). | 3, 9: **fixed** — header found at record 1 (`ALLOWED_DIFFS.md` row 4) |
| Pekao: works | — | Regression fixture `banks/pekao-like.csv` |

## 11. Known limitations (legacy §27)

| Limitation | Decision |
|---|---|
| Dedupe: files with identical names share a multiset bucket | **Already fixed in the current legacy file**: buckets are per file position (oracle `dedupe/same-name.json` keeps both identical coffees of `b/export.csv`). The rebuild buckets per import (Phase 6). |
| Dedupe: reference-less rows that differ only in category/title variants may not match; no merge/unmerge UI | Fix in phase 6/10 (duplicate overrides and the Duplicates dialog; the key ignores category) |
| Recurring detection only handles ~monthly cadence | Keep for parity (Phase 12); weekly/yearly cadence in the backlog |
| Budgets global by category name, no per-month budgets or rollover | Keep (Phase 12); per-month budgets with rollover in the backlog |
| Dates assume day-first for ambiguous `dd/mm/yyyy` | Fix in phase 9: per-import `dateOrder`, shown in the preview |
| Role overrides per view, re-mapping needed when switching views | Fix in phase 6/9: roles mapped per import (Fields menu retired) |
| Status bar Σ follows filters, balance chart ignores them | Keep; explain it in the UI copy (Phase 12) |
| Auto interface scale relies on `devicePixelRatio` heuristics | Keep (Phase 8) |
| `detectRoles().balance` unused | Keep detection; balance reconciliation in the backlog |
| CSP uses `'unsafe-inline'` | Fix in phase 8: strict CSP, no inline scripts or styles |
| No undo for removes; no transaction editing or categorisation rules | Fix in phase 15 (categories, rules); undo for import delete in the backlog |
| Large files (> 100k rows) are memory-heavy | Fix in phases 7 and 17: server-side queries, load tests |
| Tooltips mouse-only; chart values not keyboard-reachable | Fix in phases 11 and 14: table twins + focusable marks, accessibility pass |
