# 0043. The traffic simulator: a separate client that plays synthetic users on a lab

- Status: accepted
- Date: 2026-11-21

## Context

Dashboards, alerts, logs and traces are only worth their setup when something flows through them.
A personal instance is quiet most of the day, so a broken panel or a missing log field goes
unnoticed until it is needed. Phase 25 adds a service that plays about ten users, each with a
character and habits, against a lab instance all day (PLAN §9 Phase 25). It must be realistic
enough to exercise every feature area, and it must never touch the instance with real data
(the maintainer, 2026-11-16). ADR 0041 already decided that the server, not the client, says
whose traffic is synthetic.

## Decision

The eight design decisions of PLAN Phase 25, and how this repository carries them out:

1. **Go, not k6.** The personas keep state, think between actions, run for days and take orders
   from a control API. The generated client (`internal/client/gen`) covers every operation, so
   no dependency is added. k6 stays the tool for load and breaking-point tests.
2. **A separate binary and image.** `backend/cmd/finuance-sim` and `backend/internal/sim/` become
   the `finuance-sim` image, with their own chart (`deploy/helm/finuance-sim`) and namespace. The
   production image doesn't carry the simulator. The one exception is `internal/sim/identity`:
   the personas' names and the password derivation, a few functions with no imports from either
   side, so the instance's admin command and the simulator derive the same passwords.
3. **Through the front door.** The simulator calls the lab's public address (ingress, nginx, the
   API) like a browser. This exercises the whole path and opens no NetworkPolicy towards the API.
4. **The lab says so.** `FINUANCE_SIMULATION=true` makes an instance a lab: only there can synthetic
   users exist and sign in (ADR 0041), `GET /meta` reports `simulation: true`, and the web app shows
   a "Lab" chip in the title bar. The simulator refuses to start against an instance whose `/meta`
   does not report it, so it can't be pointed at the instance with real data by mistake. If it
   were, the server would still refuse every one of its users.
5. **Personas sign in like people, then work like scripts.** Each signs in once with a session
   (the cookie, `X-Finuance-Client: web`, the `Origin`), revokes its earlier `sim-*` access tokens,
   creates a `write` token (confirming its password, as every token creation does) and uses it from
   then on. That is one sign-in per persona per start, not one per action. A session is kept for
   the routes that are closed to tokens.
6. **No secret travels between the instance and the simulator.** Both get one random seed
   (`FINUANCE_SIM_SEED`, at least 32 bytes, from a Secret).
   - A persona's password is derived with HKDF-SHA256 (RFC 5869) from the seed with the info
     `finuance-sim/v1 password <slug>`: 18 bytes written as 24 URL-safe characters.
   - `finuance admin synthetic-users -count=N -seed-stdin` creates the first N personas on a lab,
     marks them synthetic and sets those passwords. It refuses outside a lab before touching
     anything, and it changes nothing when an address belongs to a person.
   - A second run with the same seed changes nothing, so it can run on every deploy. A new seed
     resets the passwords, which ends the personas' sessions and tokens.
   - `finuance-sim password <animal>` prints one password, so the maintainer can sign in as an
     animal and look at its screens.
7. **Hard ceilings in the code, not only on the page.** At most 50 personas, a maximum action rate
   per persona, at most one assistant run at a time across all personas, and back-off on `429` and
   after five consecutive `5xx`. No setting on the tuning page can exceed them.
8. **The lab's assistant talks to the mock model** (`testutil/llmmock`, published as the
   `finuance-llmmock` image for labs only) unless a real connection is configured there.

**The personas.** The ten animals of the plan are numbered in a fixed order: otter, owl, badger,
magpie, heron, hedgehog, fox, squirrel, mole, sloth. A count above ten starts the list again with
a number (`otter-2`, "Otter 2"). Each has the address `<slug>@sim.invalid`; `.invalid` is reserved
and never delivers.

**Statements on demand** (`internal/sim/statements`, task 25.2).
- **A day is a pure function.** A persona's bank history is a function of the persona and the
  calendar day. Each day's rows come from SHA-256 in counter mode over the persona's slug and the
  date, a stream whose output is fixed by its definition, unlike `math/rand`'s methods. Any range
  therefore reproduces the same rows, and overlapping statements de-duplicate as real overlapping
  exports do. `internal/demo` can't be reused: its stream starts at the beginning of the requested
  range.
- **Two accounts in two layouts.** Every persona has a current account and a savings account, one
  Pekao-like and the other mBank-like (alternating by persona). This exercises detection and saved
  import profiles, recurring payments (salary, rent, subscriptions) and transfer detection (both
  halves of each transfer, on the same day).
- **A stamp makes every upload byte-unique without changing a row.** The API answers `409` to bytes
  it already holds, for example after an abandoned preview. The simulator therefore passes a new
  stamp with every upload, which adds one line before the table:
  - `Eksport;<stamp>` above the Pekao-like header;
  - `#Eksport:;<stamp>;` in the mBank-like preamble.

  The importer takes the record right above the first data row as the header, so it reads the
  same rows with or without the stamp. The stamp keeps letters, digits, `-` and `_` only.
- **Amounts** are `int64` grosze. Merchants, employers and landlords are invented, and account
  numbers have 20 leading zeros. A year of every persona's statements passes
  `tools/check-private.sh`.

**Being easy to spot.** The trusted mark is the server's (ADR 0041). Each request also carries
`User-Agent: finuance-sim/<version> (<animal>)` and a request id `sim-<animal>-…` for people
reading logs. Those two are informative only and decide nothing. Some requests are turned away
before the session layer reads the token, and so carry no user: signing in, an unknown path, and a
path parameter in the wrong form (the generated router checks it before any middleware runs).
These count as real, as ADR 0041 says of every request without a user. The simulator's own
request ids still find them, and the coverage test allows exactly these.

## Consequences (positive, negative, follow-ups)

- Every dashboard, log query and trace view has traffic to show on a lab, labelled so that it can be
  hidden or shown, and an instance people use never counts it.
- The simulator needs a lab instance with its own database. On one machine that is a Compose
  profile (`make sim-up`). On Kubernetes it is a second release of the main chart, proven in the
  rehearsal; the homelab does not run one (the maintainer, 2026-11-21), and Phase 27's cluster will.
- Anyone holding the seed can sign in as the animals on a lab, and nowhere else (THREAT-MODEL I23).
- Two more images to build, scan, sign and attest in every release.
- The production binary carries the persona names and the derivation (`internal/sim/identity`).
  The admission policy names exact repositories per namespace, so the simulator's and the mock's
  images can't run in the namespace with real data.

## Alternatives considered

- **k6 with an externally controlled executor.** It is good at load and poor at state kept for days.
  k6 2.x also removed the externally controlled executor a tuning page would have needed.
- **The simulator calls the API's service directly.** Simpler networking, but it would skip nginx
  and the ingress, which are part of what the dashboards should show. It would also need a
  NetworkPolicy opening the API to another namespace.
- **Passwords generated by the instance and handed to the simulator.** A secret would have to travel
  from one to the other (a Secret written by a job, read by another namespace). One shared seed
  needs nothing to travel.
- **A header or token that marks traffic as synthetic.** Rejected in ADR 0041.
