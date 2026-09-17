---
name: pr-learn
description: Learn how the reviewer reviews in one repo, from PRs other people wrote that they commented on or pushed fixes to, and turn it into rules the pr-walk skill follows in that repo. Use on "/pr-learn", "learn my review style", "learn from my past reviews", "tune pr-walk for this repo", or "update my review rules".
---

# Learn a reviewer's taste, one repo at a time

pr-walk finds what is worth raising in a PR. This skill teaches it what *this
reviewer* raises *in this repo*, from the evidence of what they actually said
and changed on other people's PRs. The result is a rules file pr-walk loads
before every walk in the repo.

Rules come from evidence, not from guessing what a good reviewer would want.
Every rule you propose must point at real comments or commits.

## Step 1: gather

Run it from inside the repo (or pass `-R owner/name`), passing on any arguments
you were given (`--all` to ignore the last run's date, `--since YYYY-MM-DD`,
`--top N` to keep more or fewer PRs):

```
pr-learn
```

It does not read every PR. It scans the recent ones, scores each by how much
of the reviewer is in it (their own short line comments most, pushed commits
least), and keeps the best 40. It prints the parts of the corpus, one path per
line, and the rules file path on stderr. It takes a couple of minutes; tell the
reviewer that once, then wait. If it prints `nothing new to learn from`, say so and stop.

Read the existing rules file if there is one, and the global rules
(`~/.config/prboom/rules.md`) and the pr-walk skill's "What counts as a
finding". A rule that is already in any of them is not new.

## Step 2: read, one agent per part

Each part is a set of PRs: the reviewer's line comments with the code they sat
on, their PR comments, and small commits they pushed to someone else's branch
with the diff.

Give each part to its own subagent, all at once. Tell each one:

> Read FILE whole. It is how one reviewer reviewed other people's PRs. Find the
> patterns in what they object to and what they change.
>
> Much of the text under their name was written by their coding agent: long,
> polished, hedged explanations, "Could this…?", verification write-ups. That
> is not their voice; ignore it except as a pointer to what they asked for. Their
> own voice is short, blunt and often lowercase ("I don't think this test is
> needed", "why is this file in here?"), including instructions they address
> to the author's agent ("@someone's claude: trim the comments"). Their small
> commits count as their judgement too: what they deleted, inlined, renamed or
> replaced says what they would have flagged.
>
> Return candidate rules as a list. Each: the rule as an instruction to a
> reviewer ("Flag tests that only assert a CSS class is present"), kind
> (important, stupid or taste), and every piece of evidence as `#PR` plus the
> verbatim quote or the commit headline. Be specific to this codebase: name the
> helper, pattern, directory or library when the evidence does. Include things
> they let pass if the same thing is objected to elsewhere, as counter-evidence.
> No rule without evidence. No generic advice.

## Step 3: merge

Combine what comes back:

- Same rule from several parts: one rule, evidence pooled.
- Drop a rule seen once unless the quote is unmistakably emphatic.
- Drop what the global rules, the existing repo rules or pr-walk already say.
  If the evidence sharpens an existing rule, propose the sharper wording as an
  edit to it.
- Where the evidence contradicts itself, keep it as a question for Step 4, not
  a rule.
- Order: important, stupid, taste; most evidence first within each.

## Step 4: walk the rules with the reviewer

Four at a time, with the `AskUserQuestion` tool, one question per rule. For each:

- header: `Rule 3/17` (12 characters max)
- question: the rule, then the strongest one or two pieces of evidence, e.g.
  `Flag tests that only check CSS visibility? #3652 "Feels like bloat to me"`
- options: **keep**, **drop**, and **sharper** with your tighter or more
  specific wording in its description

What they type into Other is their wording or a correction; use it. Before the
first batch, one line: how many PRs were read and how many rules came out.

Then the contradictions, if any, asked the same way as plain questions ("You
asked for a test in #3401 but called one bloat in #3652. Which do you want
flagged?"), options from the evidence. Their answer becomes a rule.

## Step 5: write

The rules file is `~/.config/prboom/repos/OWNER/NAME.md` (the path pr-learn
printed). Create it if it isn't there. It looks like this:

```
# Review rules for OWNER/NAME

<!-- pr-learn: mined through 2026-09-17 -->

Learned from past reviews. These win over the pr-walk skill and the global rules.

## Important
- Flag a migration that backfills inline instead of in a job. (#3401, #3522)

## Stupid
- Flag tests that only assert a CSS class or visibility. (#3652, #3633)

## Taste
- Use `patch` from `@rails/request.js`, not a hand-rolled fetch wrapper. (#3599)
```

One line per rule, an instruction, PR numbers as evidence. Merge with what is
there: kept rules added under their kind, sharpened rules replacing the old
line, nothing duplicated. Set the `mined through` date to today; that is how
the next run knows where to start.

Rules the reviewer did not keep are not written anywhere. If they drop the same
rule on a later run, it will be proposed again only with new evidence.

Finish with the file path, rules added, rules changed. Nothing else.

## Style

- No preamble, no recap of what the corpus contains.
- Quote evidence verbatim. Never paraphrase a quote into something stronger.
- Never invent a rule the evidence does not show, however sensible.
