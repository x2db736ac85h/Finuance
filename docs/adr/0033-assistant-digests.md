# 0033. Assistant digests: background runs in the worker that only propose

- Status: accepted
- Date: 2026-11-06

## Context

Phase 20 lets the assistant write a short daily or weekly digest without being asked: what
happened, budgets and goals that need attention, payments due, and changes worth making. Until
now every assistant run started from a question in the panel, in the api process, while the user
watched. A digest runs with nobody watching, so it must not change anything, must not starve the
interactive use of a small model server, must stay within a cost the instance can bear, and must
be off where it makes no sense (demo visitors, an instance with the assistant switched off).

## Decision

- **Opt-in per user**, in the assistant settings (`digest`: `off` by default, `daily` or `weekly`,
  an hour, a weekday for weekly, the time zone and the connection and model the panel used when it
  was switched on). Demo users can't switch it on. An administrator can switch digests off for the
  whole instance (`features.assistantDigests`, on unless set; off with the assistant switch too).
- **Due times on `users.digest_due_at`**: when the next digest is due, worked out in the user's
  time zone (`NextDigest`, daylight saving included) whenever the settings are saved and each time
  one is scheduled. The column holds a time only, no financial data, and lets the scan find due
  users with one indexed query outside any user's context; reading the schedule itself stays under
  the user's RLS context. A worker that was down for days writes one digest, not one per missed day.
- **The worker runs digests**, not the api: a periodic `assistant_digest_scan` (every 15 minutes)
  enqueues an `assistant_digest` job per due user in the same transaction that moves the due time,
  and the jobs run on their own River queue `digest` with one worker, so each worker process writes
  one digest at a time while interactive questions keep the api's slots (the chart runs one worker
  replica). The worker gets the model connections and the model servers' egress whenever the
  assistant is on.
- **The same run loop, with read-only tools**: a fixed question (daily or weekly) goes through the
  normal runner in a new chat of kind `digest`, under `store.WithUserReadOnly` like every tool call.
  Its proposals wait in that chat for *Apply*; the panel never applies a digest's proposals by
  itself (auto-apply only follows the panel's own questions, ADR 0030). A cloud connection needs the
  user's consent as for a question.
- **A notification** `assistant.digest` (with the chat id and the number of proposals, or that it
  failed) tells the user; opening it opens the chat in the panel. Digest chats follow the user's
  chat retention and count towards the chat limit like any other chat.
- **A daily token budget per user** for every run, questions and digests alike: `assistant_usage`
  (user-owned, forced RLS) adds up each run's tokens per UTC day (the server's counts, else the
  JSON length / 4 the runner already estimates), in the same transaction that stores the answer. A
  run can't start once the day's tokens reach `assistantTokensPerDay` (`422 limit_reached`; a
  digest is skipped and noted in the log). The limit resolves like the other limits: user →
  instance → `FINUANCE_AI_TOKENS_PER_DAY` (200,000) as the outer bound.
- **"Write one now"**: `POST /assistant/digest` enqueues a digest at once (at most one waiting per
  user), for trying the setting out and for the gate.

## Consequences (positive, negative, follow-ups)

- Digests can't change anything the user hasn't confirmed, and every applied proposal is still an
  AI change with an undo.
- On a single small GPU a digest at 07:00 and a question at 07:00 share the model server; the
  question waits for the digest's current request (the server's queue), not for the whole digest.
- One digest at a time per worker replica, not per instance: a second worker replica could write
  two at once. Acceptable while the chart runs one worker; a lock is the follow-up if that changes.
- Runs started before the budget was reached finish even if they go over it; the budget is a
  daily brake, not an exact meter.
- The digest's question is English, so its answer is too (the prompt answers in the question's
  language); a language setting is a possible follow-up.

## Alternatives considered

- **Digests in the api process**: it has the connections already, but runs would compete with the
  replica's interactive slots, and with two api replicas both would need a lock to avoid writing
  the same digest twice.
- **Scanning every user's settings hourly** (the cleanup jobs' pattern): one transaction per user
  per scan for a feature few users switch on.
- **E-mail digests**: the instance sends no e-mail to users (Phase 19), and the notification keeps
  the proposals one click from *Apply*.
