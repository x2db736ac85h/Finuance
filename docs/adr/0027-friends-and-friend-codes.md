# 0027. Friends and friend codes

- Status: accepted
- Date: 2026-11-04

## Context

Phase 19 lets people share a group of expenses with someone else (PR 3). Sharing needs a deliberate
link between two accounts first, and the instance has no directory of users: people must not be
able to look each other up by e-mail address, and an address must never be shown to someone else.
The plan (PLAN §9 Phase 19, 19.7) asked for short codes valid for about an hour.

## Decision

- **Codes.** 8 characters of Crockford's Base32 (`0-9A-Z` without I, L, O, U) from `crypto/rand`,
  shown as `K7Q4-9MZD`: 40 bits. Typing is forgiving: case, spaces and dashes don't matter, and
  I/L read as 1, O as 0.
- **Lifetime.** Valid until the end of the clock hour it was made in plus 10 minutes' grace (a code
  made at 10:59 works until 11:10). A user has one code; making a new one replaces it, and a used
  code is deleted, so a code adds exactly one friend. An hourly job deletes expired codes.
- **Stored as a hash only.** `friend_codes` keeps the SHA-256, so the plain code is shown once (the
  page keeps it in memory with a countdown). Like `sessions` it sits outside row-level security: it
  is looked up by its hash before the other user is known, and only `internal/friends` reaches it
  (`store.WithoutUser`).
- **Entering a valid code makes you friends at once** (no request to accept): showing your code is
  the consent. The owner gets an in-app notification; either side can remove the friendship (the
  other side is told). `friendships` holds one row per pair (smaller id first) under forced RLS that
  lets either side see, add (only as one of the two) and delete it.
- **What friends see:** the display name and its initial, never the e-mail address. Both need a
  display name first (`422 display_name_required`).
- **Guessing.** 10 tries an hour per user and per client address (`429`), every failure in the
  Activity (`friend.code_failed`), one answer for wrong, expired and own codes. With at most a
  handful of live codes at a time, 10 guesses an hour hit one with a chance around 10⁻¹¹.
- **Switches.** Off when an administrator turns Friends off (the routes answer `404`) and always off
  in demo mode.

## Consequences (positive, negative, follow-ups)

- No user directory and no e-mail addresses between people, at the cost of exchanging a code
  through another channel (in person, a message).
- A code a user reloads away is gone; they make a new one (it costs nothing).
- 40 bits is enough only because of the short lifetime and the rate limits; a leaked database holds
  hashes that are brute-forceable offline, but every code in it expires within 70 minutes.
- The limits live in memory per api replica, like the other limits (THREAT-MODEL §5).
- PR 3 builds on it: shared groups invite friends only.

## Alternatives considered

- **Invite by e-mail address**: needs a lookup by address (an enumeration oracle) and e-mail sending,
  which the app doesn't have.
- **Friend requests to accept**: one more step for the same consent; the code already is the
  consent.
- **Codes kept encrypted so the same code can be shown again**: needs an instance encryption key
  (planned for Phase 20's API keys); chosen against on 2026-11-03.
