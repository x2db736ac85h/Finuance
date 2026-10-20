---
description: Start a plan phase — read the plan and progress, then propose a task breakdown
argument-hint: <phase number, e.g. 3>
---

Start Phase $ARGUMENTS of the Finuance rebuild.

1. Read `docs/PROGRESS.md`. Confirm that the previous phase's gate has been approved. If not, stop and say so.
2. Read the "Phase $ARGUMENTS" section of `docs/PLAN.md` §9 and every reference under its "Read first", including the `legacy/DESIGN.md` sections and the legacy functions it names.
3. Enter plan mode. Propose:
   - a numbered task breakdown that matches the phase's tasks: files to create or touch, tests to write, commands to verify each task;
   - dependencies that need approval (anything not in PLAN §2 or §7.2);
   - open questions and ambiguities. Ask instead of guessing;
   - risks against the phase's size estimate.
4. Wait for approval. Then create the branch `phase/NN-short-name` from an up-to-date `main`, set "Current phase" in `docs/PROGRESS.md`, and list the phase tasks there.
