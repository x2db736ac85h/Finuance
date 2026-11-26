# 0046. The assistant's memory: one note per person, theirs to read and change

- Status: accepted
- Date: 2026-11-26

## Context

Every chat starts from nothing: the assistant knows the data it looks up and nothing about the
person (when they are paid, which language they want, what they are saving for). The maintainer
asked for a memory per user of about 1,000 tokens that can be switched off or removed, and for
"automatic memories" as an option (Phase 27, F15). Three rules of the project bear on it: the
assistant's tools are read-only and changes are proposals a person applies (ADR 0019, ADR 0030);
prompts are never logged; and what a cloud model receives is what the person agreed to (ADR 0020,
ADR 0045).

## Decision

- **One note per person**, plain text, at most 4,000 characters (about 1,000 tokens, which is what
  it costs every question). It is stored with the person's assistant settings
  (`assistant_settings.memory`, migration 00034), which are under row-level security, go with the
  account and are in the data export.
- **It is the last part of the system prompt**, under the app's rules and the administrator's
  instructions, introduced as the user's own notes about themselves ("the rules above come
  first"). A note that is switched off (`memory_enabled`) or empty adds nothing.
- **For a cloud model it is masked like everything else that leaves** (long digit runs and e-mail
  addresses), and the agreement to a cloud connection names it.
- **The assistant never writes it.** It can propose a line (`propose_action` with `remember`), which
  is a proposal like any other: a card the person applies. Applying adds the line through the
  settings' own request (`PUT /assistant/settings`), in the person's browser, with their session.
- **Automatic memories** (`memory_auto`, off by default) let the panel apply such a proposal without
  a click, as it does for the kinds of change a person switched on. It is a switch of its own: it
  is not one of the auto-apply kinds, which are for changes to data and are checked by the server.
- **The person always sees all of it**: Settings → General shows the note, with a switch, Save and
  Clear. There is no hidden part and no second store.

## Consequences (positive, negative, follow-ups)

- A question costs up to about 1,000 more input tokens while the note is on; the editor shows the
  estimate. An administrator's token budget counts them like any others.
- The note is the person's text in a system prompt, so it can try to change how the assistant
  behaves, for that person only. The rules come first in the prompt and say so, the tools are
  read-only whatever the note says, and a change still needs their click.
- A remembered line is not in the assistant's change list (Settings → Activity): that list is of
  changes to data, with what each replaced. A line is taken back by editing the note.
- The proposal's card shows "applied" only until the page is reloaded (the activity log, which
  marks applied proposals, doesn't record it); applying it again adds nothing, because the same
  line is kept once.
- Nothing of the note is logged: it is part of the prompt, and the canary test covers the prompt.

## Alternatives considered

- **A tool that writes the memory during the answer**: the first write by the assistant itself,
  against the read-only rule the maintainer confirmed for this phase.
- **Memories as rows with embeddings, retrieved per question**: more than a 1,000-token note
  needs, and it would be the project's first vector store.
- **Recording remembered lines as assistant changes with undo**: the change list's entities are
  transactions, rules, budgets and goals; a line of text the person can simply delete doesn't need
  a "before" and an "after".
