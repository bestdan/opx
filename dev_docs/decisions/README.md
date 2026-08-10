# Decisions

Why the things `opx` **didn't** build, didn't get built — and what would
change the answer.

`AGENTS.md`'s "Things to avoid" is the index of those refusals, and it is
deliberately terse: one entry, one conclusion. That works as a rule and fails
as an archive. When someone proposes the same shape a year later — reasonably,
because it is usually a good idea in the abstract — the conclusion alone
cannot tell them whether the objection still holds. This directory holds the
argument behind each conclusion, so a proposal can be answered on its merits
instead of by citing a line.

Read this file first; open a decision only when the summary below isn't
enough. That is the whole point of the split.

## How this differs from the other two reasoning docs

| File | Answers |
| ---- | ------- |
| [`../security-hardening.md`](../security-hardening.md) | Why the invariants that **shipped** are shaped the way they are, including the premises that turned out wrong on the way |
| [`../designs/`](../designs/) | Evaluations of external things — does X replace `opx`, should `opx` adopt Y |
| this directory | Why a proposed change to `opx` itself was **rejected**, and what would reopen it |

A decision here is terminal by construction. It is dated in its own header
and is allowed to be stale forever: it records what was decided and why, not
what is currently true. If the reasoning stops holding, that is a *new*
decision — add one and mark the old one superseded rather than editing
history.

## The decisions

| Decision | Status | Decided | In one line |
| -------- | ------ | ------- | ----------- |
| [A broker between the caller and `op`](./broker-between-caller-and-op.md) | Rejected | 2026-08 | The only shape that lets a sandbox admit `opx` but block raw `op` — but it buys ergonomics, not security, and its prompt-fatigue answer is the session cache renamed |

## Adding one

Name the file for its **topic**, not its date — `broker-between-caller-and-op.md`,
not `2026-08-10-broker.md`. The date is low-signal in a filename and lives in
the header and the table above, where it can be read without opening anything.

Keep the skeleton: **Decision / Status / Decided / Context / Why it was
rejected / What would reopen it**. The last section is the one that earns the
file — a rejection without a reopen condition is a dead end rather than a
decision, and it is what tells a future reader whether to re-litigate.

Add a row to the table above in the same change, and link the decision from
its `AGENTS.md` "Things to avoid" entry so the terse rule and the argument
stay connected in both directions.
