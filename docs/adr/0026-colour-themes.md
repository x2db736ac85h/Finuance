# 0026. Colour themes and the theme editor

- Status: accepted
- Date: 2026-11-03

## Context

The app had one look (the legacy palette, raised to WCAG AA in Phase 14) in a dark and a light
variant. Phase 19 asks for colour themes in the spirit of popular desktop themes, four to choose
from, each in both variants, and an editor with which administrators make and approve more (PLAN
§9 Phase 19, 19.4–19.5). Three constraints: every theme must stay accessible (the axe pass has zero
serious violations), a theme must not be a way to inject anything into the page, and the
Content-Security-Policy (`style-src 'self'`) must not loosen.

## Decision

- **Contract.** A theme is 16 colour tokens per variant (`web/src/styles/theme-contract.json`): the
  surfaces (`bg`, `panel`, `elev`, `input`, `border`), text (`text`, `muted`), accent (`accent`,
  `accent-solid`, `on-accent`, `accent-soft`, `sel`), money (`pos`, `neg`) and the logo
  (`logo-a`, `logo-b`). The contract also lists the text/surface pairs that must reach 4.5:1; a
  translucent tint is checked laid over the page background. The server keeps the same lists
  (`internal/instance/theme.go`), and a Go test compares them with the JSON file.
- **What a theme doesn't change**: the categorical chart palette and the heat-map steps, which are
  validated for colour-blind readers and stay the same in every theme. The syntax colours of the
  column-type badges (`--c-num`, `--c-date`, `--c-text`) keep their hues, but a built-in theme
  darkens or lightens them where Indigo's would read below 4.5:1 on its header (`themes.test.ts`
  checks every theme and mode).
- **Built-in themes** live in `tokens.css` (keyed by `data-theme-id` and `data-theme`) and in
  `themes.ts` (a test keeps them equal): **Indigo** (the existing look, the default), **Fjord**
  (cool frost blue on slate, after Nord), **Grove** (sage and aqua, after Everforest; its accent
  stays clear of the income green), **Ember** (warm gold, after Gruvbox; filled buttons carry
  dark text). Names are our own; the inspirations are credited in the description. Every pair of
  every variant passes (`themes.test.ts`), and the axe pass runs table, overview and settings in
  each of them.
- **Administrator themes** are rows in `themes` (draft → approved → retired; versioned). They start
  as a copy of a built-in theme. Values must be lowercase `#rrggbb` or `#rrggbbaa` for exactly the
  contract's keys, so a theme can carry no other CSS. Approval runs the contrast check again on the
  server (`422 contrast_failed` lists the pairs), and an approved theme's new colours must pass too.
- **Applying a theme.** Built-in themes switch with the `data-theme-id` attribute. An approved
  theme sits on the Indigo base and sets its 16 tokens with `style.setProperty` on `<html>`;
  CSSOM changes are allowed under `style-src 'self'`. Users choose with the `themeId` preference;
  an unknown or retired theme falls back to the instance's default, then to Indigo.

## Consequences (positive, negative, follow-ups)

- New themes need no deploy and can't break accessibility or the CSP.
- An administrator theme inherits Indigo's secondary greys (`--bg-2`, `--border-2`, `--text-2`) and
  syntax colours, which aren't in the contract; that is usually close enough, and the contract can
  grow later.
- The chat send button now uses the solid accent with `--on-accent`, as filled buttons do (Indigo
  shows a slightly deeper violet there than before).
- Every axe run covers 4 × 2 combinations of three screens, which adds about two minutes to the E2E job.

## Alternatives considered

- **Arbitrary CSS per theme**: needs `unsafe-inline` or a stylesheet endpoint, and can't be checked
  for contrast.
- **Deriving every colour from one accent with `color-mix()`**: fewer inputs, but the built-in
  themes' surfaces and text colours aren't functions of their accent, and the result is harder to
  check.
- **Per-theme chart palettes**: more expressive, but each would need its own colour-blind
  validation; kept for later.
