---
name: pr-walk
description: Walk the reviewer through a pull request one item at a time, putting the relevant code in the pane beside them and asking fix / comment / next on each. Use on "/pr-walk", "walk me through this PR", "review PR 1234", or when the task at hand is a PR review. Runs in a worktree with the code pane (prboom view) alongside, made by pr-open or by TaskYou.
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
cat ~/.config/prboom/repos/"$(gh repo view --json nameWithOwner -q .nameWithOwner)".md 2>/dev/null
```

The last is what `pr-learn` learned from the reviewer's past reviews in this
repo. It is the most specific, so it wins over the other two.

Then work out the base and the size:

```
BASE=$(cat "$(git rev-parse --path-format=absolute --git-dir)/pr-review-base" 2>/dev/null || echo origin/main)
git diff --stat "$BASE..."
```

The pane beside you is the code pane (`prboom view`). You put code in it with
`pr-show`; the reviewer can also search it, open any changed file, leave notes
for the author, and ask you about a line. If `pr-show` errors, say so once and
carry on without showing code.

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

Before asking, give the code pane the list, in walking order, one per line as
`FILE:LINE title`. It shows which finding is on screen and lets the reviewer
step through them. A grouped finding goes in once, at its first place:

```
prboom findings <<'LIST'
app/models/offer.rb:120 Delete recalculate_legacy (-21)
app/services/payout.rb:44 Rounds before converting currency
LIST
```

If there are none, say so and stop instead: `Nothing worth your time here.`

## Step 2: one finding at a time

For each finding, in this order, worst first:

**First** put the code in their pane:

```
pr-show app/models/offer.rb:120
```

Use the same `FILE:LINE` you gave `prboom findings`, so the pane knows which
finding this is.

**Always pass a line number.** Without one the pane shows the file's whole
diff. With one, it shows the lines around it with the PR's changes in place and
the cursor on that line.

A second argument sets the lines either side, default 30. Use 10 to 15 when the
point is one expression; more only when they need the surrounding method.

`pr-show` never touches their keyboard, and works the same in a pr-open window
and a TaskYou task.

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

- **fix** — make the edit now, in this worktree. The code pane reloads the file
  by itself; `pr-show <file>:<line>` if the change is away from what is on
  screen. One line saying what changed. Next finding.
- **comment** — they dictate or approve a note for the author. Save it, do not
  post: `prboom comment app/models/offer.rb:120 "The note"` (with `-left` before
  the location for a line the PR deletes). It shows up in the code pane. No code
  changes. Next finding.
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

**A message that starts `From the code pane, about FILE:LINE`** is the reviewer
asking from the pane, with the line quoted. Treat it like Other: answer it or do
it, then ask the decision you were on again. It does not answer that decision.

## Step 4: finish

When the list is done, output:

- one line per decision, grouped: fixed, commented, passed over
- the net line change: `git diff --stat "$BASE..."`
- anything you were unsure about, max three lines

Then gather the notes for the author. The reviewer may have left their own in
the code pane besides the ones you saved:

```
prboom comments --json
```

Each has `path`, `line`, `side` (`RIGHT`, or `LEFT` for a deleted line),
`body`, `code` (the line as it read) and `by` (`you` is the reviewer, `agent`
is you). Keep the reviewer's words as they wrote them.

If there are any, print the exact review that would be posted, in full, as
message text: each note under its `path:line`, then the summary body if there
is one. Then offer to post it with `AskUserQuestion` (options **Post** and
**Don't post**). Do not post without being told. A draft that only exists in a
file or a tool call has not been shown: they cannot approve what they have not
read. Every time the draft changes, print it again before asking again.

Post it as one review with each note on its line, not as a single PR comment:

```
head=$(gh pr view <N> --json headRefOid -q .headRefOid)
prboom comments --json | jq --arg head "$head" --arg body "<summary, or empty>" '{
  commit_id: $head, event: "COMMENT", body: $body,
  comments: map({path, line, side, body})
}' | gh api "repos/{owner}/{repo}/pulls/<N>/reviews" --input -
```

GitHub only takes a line comment on a line inside the PR's diff. If it answers
422, move the notes it refused into the review body as `` `path:line` note ``
and post again. A note on a file you have since edited may point at a shifted
line: compare `code` with `git show HEAD:<path>` and correct `line` first.

After it posts, `prboom comments --clear`.

## What counts as a finding

Assume the PR may be machine-written and nobody read it closely. Read every
changed line, not a sample. Before calling anything non-idiomatic, read how the
neighbouring code in this repo does the same job, and grep for an existing
helper, concern or pattern the PR could have used.

Three kinds, walked in this order:

**1. Important.** Wrong behaviour, data loss or corruption, security or auth
holes, races, N+1s on a hot path, migrations that lock or can't roll back, errors
swallowed or silently replaced with degraded data, behaviour changes with no
test, and tests that would still pass if the implementation were deleted.

**2. Stupid.** Things a careful person would never have committed: dead code
nothing calls, leftover debug output, commented-out code, TODOs, unrelated
files or drive-by refactors, a reimplementation of something the repo or
framework already has, copy-paste duplication, an abstraction, option or config
flag with one caller, defensive checks for states that can't happen, comments
that narrate what the next line does, and files or methods bloated far past
what the job needs.

**3. Taste.** Code that works but isn't how this codebase or its framework does
it: hand-rolled what the framework provides, a new pattern where the repo
already has one, clever where explicit would do, regex or string matching for
a problem that isn't about string shape, and names that mislead.

Not findings: formatting, anything a linter or CI catches, or anything you
would phrase as "consider". A taste finding must point at the idiom it should
have used, in this repo or the framework; "I'd have done it differently" is
not one.

Group repeats. If the same smell appears in six places, that is one finding
listing the six, fixed together.

Raise every finding that clears the bar; do not cap or trim the list. If the
PR is clean, say so in one line and stop.

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
