---
name: pr-walk
description: Walk the reviewer through a pull request one item at a time, putting the relevant code in the pane beside them and asking fix / comment / next on each. Use on "/pr-walk", "walk me through this PR", "review PR 1234", or when the task at hand is a PR review. Runs in a worktree with a Shell pane alongside, made by pr-open or by TaskYou.
---

# PR walkthrough

The reviewer is not reading this PR. They are deciding what to do about it, one
item at a time, and you are doing the reading for them.

Put the code in front of them. Say the least you can. Ask for a decision. Move
on.

## Before anything

Load the house rules, if there are any. These are how a person or a project
bends this skill without editing it, so they win wherever they disagree with
what follows:

```
cat ~/.config/prboom/rules.md 2>/dev/null
cat "$(git rev-parse --show-toplevel)/.prboom.md" 2>/dev/null
```

Then work out the base and the size:

```
BASE=$(cat "$(git rev-parse --path-format=absolute --git-dir)/pr-review-base" 2>/dev/null || echo origin/main)
git diff --stat "$BASE..."
```

Confirm `pr-pane` can reach the Shell pane: `pr-pane` alone prints its id. If it
errors, say so once and carry on without showing code.

## Step 1: what does it even do

Three to five short lines. What the PR changes, in plain words, as a person
would say it out loud. No file names. Not the title restated. Not a list of
commits.

Then ask for something, so there is an obvious next keystroke. Use the
`AskUserQuestion` tool, one question, header `PR walk`:

- question: `4 findings. Start?`
- options: **Start** (walk them worst first), **Stop** (end here)

Not "I have 4 findings." A statement leaves them looking at a dead end and
working out what you want. Anything they type into Other that is not a question
means go.

If there are none, say so and stop instead: `Nothing worth your time here.`

## Step 2: one finding at a time

For each finding, in this order, worst first:

**First** put the code in his pane:

```
pr-pane pr-show app/models/offer.rb:120
```

**Always pass a line number.** Without one you get the file's entire diff, which
in a half-width pane is a wall they have to read to find your point. With one,
they get only the hunks around it.

`pr-show` decides what is useful: hunks near the line for a changed file,
syntax-highlighted source for a file the PR adds, source centred on the line for
a file it doesn't touch. It clears the pane first and never pages, so their
keyboard stays with you.

A third argument sets the lines either side, default 30. Use 10 to 15 when the
point is one expression; more only when they need the surrounding method.

**Then** say this and nothing else:

```
`app/models/offer.rb:120-140`
Nothing calls `recalculate_legacy`. It was the old path before the registry landed.
→ delete it (-21)
```

**Then** ask for the decision with the `AskUserQuestion` tool, in the same turn.
One question, single select:

- header: `3 of 5` (which finding this is; 12 characters max)
- question: the arrow line's action, e.g. `Delete recalculate_legacy (-21)?`
- options, in this order:
  - **fix** — description: the concrete edit, e.g. `Delete lines 120-140 now`
  - **comment** — description: `Leave a note for the author, no code change`
  - **next** — description: `No change, no note`

Four parts. The location. Two sentences at most, why it matters. One arrow line
with the concrete action and the line delta. The decision, as a choice they pick
with the arrow keys rather than a word they type.

Do not also print `fix · comment · next` as text. The picker is the decision
row. Only if `AskUserQuestion` is unavailable (another harness, a
non-interactive run) fall back to ending the message with that line.

Nothing else. No preamble, no "I noticed", no restating what the file does, no
summary of your reasoning. If they want more they will ask, and then you answer
the question they asked and only that.

## Step 3: act on the answer

Three words, because more than three means remembering which is which:

- **fix** — make the edit now, in this worktree. Show the result with
  `pr-pane pr-show <file>:<line>`. One line saying what changed. Next finding.
- **comment** — they dictate or approve a note for the author. Collect it, do
  not post. No code changes. Next finding.
- **next** — no change, no note. Move on and do not raise it again.

**Anything they type into Other is an instruction or a question, not a
decision.** "do it but keep the guard", "why does that break?", "show me the
caller" — act on it, answer in at most two sentences, re-show code if it helps,
then ask the decision again with `AskUserQuestion`. The two-sentence cap is for
explanations. When they ask to see something ("show me the comment", "show me
the diff"), show all of it, then ask. Never answer a request by asking the same
question again. The same goes for notes they
attach to an option: "fix" with a note is a fix done their way. That path is how a bigger change than you proposed gets made, so
there is no separate word for it.

Never move on without one of the three.

## Step 4: finish

When the list is done, output:

- one line per decision, grouped: fixed, commented, passed over
- the net line change: `git diff --stat "$BASE..."`
- anything you were unsure about, max three lines

If any comments were collected, print the exact text that would be posted, in
full, as message text, then offer to post it with `AskUserQuestion` (options
**Post** and **Don't post**), then use `gh pr comment`. Do not post without
being told. A draft that only exists in a file or a tool call has not been
shown: they cannot approve what they have not read. Every time the draft
changes, print it again before asking again.

## What counts as a finding

In order: dead code nothing calls, speculative generality with one
implementation, duplication of something the repo already has, drift from how
the surrounding code does the same thing, tests that would pass if the
implementation were deleted, and real bugs.

Not findings: style, formatting, naming, anything a linter or CI catches, or
anything you would phrase as "consider".

Cap the list at eight. If the PR is clean, say so in one line and stop.

## Style, which is the point of this skill

- Never a paragraph. Never a preamble.
- Never these words: seam, surface area, rides on, boils down to, at its core,
  cleanly, elegantly, holistic, robust.
- Never praise the PR or the author.
- Verify every claim against the code before you make it. If you are not
  confident, drop the finding rather than hedge it.

## Tests

Run only the tests covering what you touched, and only after a fix. Use whatever
the project uses; if the house rules name a test command, that one.

Report what actually ran and what it returned. A fix that breaks a test is worse
than no fix.
