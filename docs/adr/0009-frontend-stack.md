# 0009. Frontend stack: React + Vite, TanStack Query/Virtual, Zustand, CSS Modules with legacy tokens, no chart or grid libraries

- Status: accepted
- Date: 2026-10-20

## Context

The legacy UI is a dense, IDE-like interface (VS Code / Obsidian / LM Studio look) built with template strings and `innerHTML`. Its design and behaviour are validated and must be preserved. We need a component model that supports a strict CSP (no `innerHTML`), typed server state, and a maintainable codebase that AI tools and future contributors know well.

## Decision

- **React + TypeScript (strict) + Vite**, as an SPA with **React Router** in library mode.
- **Server state:** TanStack Query, with hooks in `src/api/hooks` over the generated `openapi-fetch` client. Components never call `fetch`.
- **Client UI state:** Zustand stores (`viewStore`, `uiStore`, `selectionStore`), mirroring legacy `S.views`. Persisted preferences sync to the server.
- **Grid:** a custom grid on **TanStack Virtual**, with no data-grid library, so the legacy look and keyboard behaviour carry over exactly.
- **Charts:** the legacy SVG chart code, ported to React components. No chart library. Every chart has a mandatory table twin (`toTable`) for accessibility and the modal.
- **Popovers and tooltips:** `@floating-ui/react` replaces the hand-rolled `placePop`.
- **Styling:** plain CSS. The legacy design tokens become CSS variables in `src/styles/tokens.css`, with CSS Modules per component and rem-based scaling (UI scale incl. auto). No Tailwind, no CSS-in-JS.
- **Markdown** in assistant answers: a port of the legacy `md()` to a React-node renderer. No `dangerouslySetInnerHTML`, ever.
- **Tests:** Vitest + Testing Library + MSW, plus Playwright for E2E.
- Exact version pins. New dependencies need approval.

## Consequences (positive, negative, follow-ups)

- Visual parity is achievable because we port the CSS and SVG maths rather than re-theme a library.
- A strict CSP (`script-src 'self'`, `connect-src 'self'`) is possible because nothing injects HTML.
- We own more code (grid, charts) than with libraries. The legacy implementation reduces the risk, and the oracle and parity harness (Phases 1 and 14) check behaviour.
- The small dependency surface means fewer supply-chain risks and Renovate PRs.

## Alternatives considered

- **Svelte / SolidJS:** smaller and faster, but less familiar to employers and AI tools.
- **AG Grid / TanStack Table UI kits:** heavy, and hard to make look like the legacy grid.
- **Recharts / ECharts / visx:** quick to start, but the legacy charts are already validated and distinctive, and libraries complicate the CSP.
- **Redux Toolkit:** more ceremony than the state needs. Zustand matches the legacy `S` object closely.
- **Tailwind:** would mean re-expressing every legacy token and rule in utilities, which makes parity harder.
