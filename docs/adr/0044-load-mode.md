# 0044. Load mode: the animals load a lab and replace the load-test users

- Status: accepted
- Date: 2026-11-23
- Changes: decision 7 of [ADR 0043](0043-traffic-simulator.md) (hard ceilings, no load generator)

## Context

ADR 0043 kept the simulator realistic on purpose: at most 50 personas, at most 30 actions a minute
each and 3× the characters' pace, so that it "can't be turned into a load generator". k6 did the
load tests, with a separate set of synthetic users seeded with 20,000 transactions each
(`seed-demo -users -synthetic`, task 25.10).

The day in the lab (task 25.12, `docs/perf.md`) ran ten animals for 24 hours: no `5xx`, but about
three requests a minute. That can't show how the system behaves under pressure, and it couldn't
decide the overview cache. The maintainer asked on 2026-11-23 for the animals to be able to load
the system, to replace the load-test users, and to show what the load costs.

## Decision

1. **Two modes.** *Realistic* is ADR 0043's behaviour with its ceilings, and stays the default.
   *Load* paces the animals by one lab-wide target instead of their think times and daily curves.
2. **Load mode has ceilings in the code too.** No setting on the page can pass them:
   - at most **500 animals** (`identity.Max`; the names continue `otter-2 …`, and existing
     passwords don't change);
   - at most **2 actions a second per animal**. The instance allows an access token 600 requests a
     minute, and an action makes about three;
   - a lab-wide target of at most **1,000 actions a second**, with bursts of at most one second's
     worth above it;
   - a **duration**: load mode ends by itself and returns to realistic mode, after 30 minutes by
     default and 24 hours at most.
3. **What doesn't change in either mode:**
   - the refusal of a target that isn't a lab;
   - the control token;
   - the assistant's limits (one run at a time across all animals, the daily cap);
   - `Retry-After` on `429`, and a new sign-in after `401`.

   The back-off after five `5xx` in a row stays on, unless load mode's "push through errors" is
   chosen, to find a breaking point.
4. **The lab stays like production.** Its limits per address are not relaxed. Every animal comes
   through the same front door, so the simulator paces its sign-ins under the instance's sign-in
   limit (25 at once, then one every 2.5 seconds), and 500 animals join over about 20 minutes. A
   mistyped password takes a sign-in turn too. Each animal is a user of its own and keeps below the
   instance's limits per user at their defaults: a question to the assistant every two minutes at
   most (30 an hour), an upload every 2.5 seconds (it waits; 30 a minute), an invitation every 2.5
   minutes (30 an hour). The first load runs found each of these as `429`s.
5. **Bounded telemetry.**
   - The simulator's metrics are labelled by **animal** (ten values), not by persona: 500
     personas would make tens of thousands of series. The page keeps the per-persona detail.
   - The feed is sampled under load, at most 50 actions a second to each viewer; the totals stay
     exact.
   - The simulator keeps up to 256 idle connections to the lab, so a thousand requests a second
     don't use up its local ports (as nginx's did in task 20.21).
6. **Randomness is a setting.**
   - A **run seed** enters every animal's stream. 0 draws a new one at start, and the page shows the
     seed in use, so a run can be repeated.
   - **Jitter** spreads think times around the character's mean, and in load mode around the
     pacer's interval.
   - **Bursts** start a few quick actions in a row; in load mode they deepen the pacer's bucket.
   - The **mistake share** stays as it was.
7. **One set of synthetic users.**
   - The animals replace the k6 load-test users. A **history profile** (rows a day, years back,
     amount spread, merchants and categories) gives them load-test-sized data through the normal
     upload and commit.
   - The profile is part of the statements' stream and of the account's name. A restarted
     simulator reads it back, and overlapping statements still de-duplicate.
   - `finuance-sim seed` builds the histories without the page, and `finuance-sim users` writes the
     animals' credentials for k6 (a file of mode 0600).
   - `seed-demo -synthetic` goes. `seed-demo -users` stays for instances that aren't labs, such as
     the homelab, where load-test users are people.

## Consequences (positive, negative, follow-ups)

- One tool loads a lab with every feature area, not only the table and the charts. Its cost shows
  on the dashboard "Finuance · Lab resources" and on the page.
- A lab can now be saturated on purpose. That is the point, and it stays a lab: the target check
  comes first, the control token guards the page, and load mode ends by itself (THREAT-MODEL D14,
  B10).
- k6 stays for CI's smoke run, the rollout drills (pod kills) and an instance that isn't a lab. Its
  scripts sign in as animals on a lab.
- The lab makes as many animals as load mode may play: 100 by default on Compose
  (`SIM_ANIMALS`, at most 500), and a chart value on Kubernetes.
- The dashboard "Finuance · Simulator" groups by animal; with the default ten animals it looks as
  before.

## Alternatives considered

- **Keep the ceilings and let the page start k6 runs.** That gives two kinds of synthetic user and
  two kinds of traffic. k6's load is the table and the charts only, and the page would need a way to
  run k6 (a container started from the simulator, the Docker socket).
- **No ceilings in load mode.** A mistyped target rate would take the development machine with the
  lab. The ceilings are well above that machine's breaking point (about 100 requests a second,
  Phase 18) and still bounded.
- **Relax the lab's limits per address.** Faster start-ups, but a lab would then answer differently
  from production, and the limits are part of what the lab tests.
