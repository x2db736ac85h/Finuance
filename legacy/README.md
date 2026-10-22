# The legacy prototype

`finuance.html` is the original single-file version of Finuance: one HTML file with all of its CSS and JavaScript, which runs entirely in the browser and keeps its data in the browser's storage (IndexedDB, optionally encrypted with a passphrase). Open it in a browser to try it; it needs no server.

The application in this repository is a full rebuild of it as a multi-user web app (Go, PostgreSQL, React), and this folder is kept as its reference:

- [`DESIGN.md`](DESIGN.md) documents how the prototype works, feature by feature.
- [`../tools/legacy-oracle`](../tools/legacy-oracle) runs the prototype in a headless browser and records its results, so the rebuild can be tested against them.
- [`../docs/PARITY.md`](../docs/PARITY.md) lists every capability of the prototype and the phase that ports (or deliberately retires) it.

The prototype is no longer developed.
