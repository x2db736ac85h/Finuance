---
description: Close the current phase — run every check, update docs, draft the PR with the gate checklist
---

Close the current phase (see "Current phase" in `docs/PROGRESS.md`).

1. Run `make check` and `make test-integration`. If anything fails, fix it. Never skip, delete or weaken a test; if the spec looks wrong, stop and ask.
2. Walk through the phase's **Acceptance criteria** in `docs/PLAN.md` §9. Show evidence for each: a command and its output, a file path, a screenshot.
3. Update `docs/PROGRESS.md`: tick tasks, record deviations with reasons, move out-of-scope ideas to the Backlog, list follow-ups, add a session-log line.
4. Update `docs/PARITY.md` statuses for any legacy features touched, and add ADRs for new decisions.
5. Draft the PR description using `.github/pull_request_template.md`. Include the phase's **Gate** block as a checklist for Irwin, and screenshots in dark and light themes if UI changed.
6. Push the branch and open the PR against `main`. Merge with a merge commit (`--no-ff`) only after Irwin approves the gate.
