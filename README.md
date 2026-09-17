# prboom

Pick a pull request and walk it one finding at a time, with the relevant code in
the pane next to you.

```
prboom          # PRs waiting on your review
prboom -a       # every open PR
prboom -l       # print the list and exit, no picker
prboom -A login # only PRs by that author (@me works)
prboom -s loc   # sort by recency (default), author, loc, or files
prboom -R owner/name
prboom 1234     # no picker: walk PR 1234 now (same as ⏎ on it)
prboom 1234 -ty # same, as a TaskYou task opened in ty (same as t on it)
```

It opens the way you left it in that repo: the same list, sort, `/` filter and
`f` author. A flag given this time (`-a`, `-s`) wins over what was saved.

| Key | Does |
|---|---|
| `↑` `↓` `j` `k` `g` `G` | move |
| `1` `2` `3` | review requested, all open, mine (`a` still toggles the first two) |
| `/` | filter by title, author, branch or `#number`; `esc` clears it |
| `f` | show only this PR's author; again for everyone |
| `s` | cycle the sort: recency, author, loc, files (biggest first) |
| `tab` | detail pane: overview, files, diff |
| `J` `K` `space` `^d` `^u` | scroll the detail pane |
| `p` | hide or show the detail pane |
| `⏎` | walk it now: worktree + tmux window + agent |
| `t` | same, but tracked as a TaskYou task, and opened in ty; reopens the task if there already is one |
| `d` | the whole diff in delta, side by side |
| `x` | close the walk you have open on it: session, worktree, branch |
| `L` | learn how you review this repo (`/pr-learn`), then back to the list |
| `o` | open it on GitHub |
| `y` | copy its URL |
| `r` | refresh |
| `?` | every key |
| `q` | quit |

The list sits beside a detail pane (under it, in a narrow terminal) that shows
the description, reviewers, files, and the diff through delta, for whichever PR
the cursor rests on. `⏎`, `t` and `d` hand the terminal over and come back to
the list when they finish. It does not judge the PRs for you.

It opens on the last list it fetched while gh fetches a fresh one, so there is
something to read at once; left open, it refreshes every few minutes.

A PR you are already walking is marked beside its number: `ty 5421` for an
unfinished TaskYou review task in this repo's project, `open` for a `pr-open`
worktree.

## The walkthrough

Enter gives you two panes: the agent on the left, the code pane on the right.
The agent loads the `pr-walk` skill and, for each finding, puts the code in the
right pane *before* saying anything about it, then says four things:

```
`app/models/offer.rb:120-140`
Nothing calls `recalculate_legacy`. It was the old path before the registry landed.
→ delete it (-21)

fix · comment · next
```

`fix` edits the worktree now. `comment` collects a note for the author. `next`
moves on. Anything else you type is an instruction or a question, which is how a
bigger change than it proposed gets made.

Nothing pages, so your keyboard stays with the agent and a decision never costs
a pane switch.

## The code pane

The right pane is `prboom view`. The agent points it at a line with `pr-show`,
and it shows the file with the PR's changes in place, the cursor on that line,
and which finding this is. Click into it, or `ctrl-b →`, when you want more
than the agent showed:

| Key | Does |
|---|---|
| `j` `k` `^d` `^u` `g` `G`, wheel, click | move, scroll, put the cursor on a line |
| `tab` | around the line · just the diff · the whole file |
| `/` `n` `N` `esc` | search, next, previous, clear |
| `f` | open any file the PR changes |
| `[` `]` | previous or next finding |
| `.` | back to what the agent showed |
| `c` | a note for the author on this line; again to edit, empty to remove |
| `a` | ask the agent about this line: pasted into its pane with the line quoted |
| `r` | reload |
| `q` | quit; the pane becomes a shell |

Nothing here moves the walk. You still answer the agent in its own pane.

Your notes and the ones the agent saves on `comment` go in one list, and at the
end the agent posts them as a single GitHub review, each note on its line, once
you have read the draft and said post.

It shows fixes made during the walk without a commit, and reloads the file on
screen when it changes. Everything it knows is in the worktree's git dir under
`prboom/`, so a pane restarted mid-walk picks up where it was, and removing the
worktree removes it.

## Two ways in, same shape

`⏎` runs `pr-open`, which needs **only git and tmux**. `t` runs `pr-task`, which
builds the same two panes through TaskYou and puts a card on the board. The skill
works under either: `pr-show` looks for the pane titled `Shell`, and if it is
sitting at a prompt, turns it into the code pane. A pane busy running something
is left alone.

## Pieces

| | |
|---|---|
| `prboom` | the picker |
| `pr-open N` | worktree + tmux window + agent, no other dependencies |
| `pr-task N` | the same via TaskYou, tracked on the board |
| `pr-show FILE:LINE [CTX]` | points the code pane at a line (`prboom show`) |
| `prboom view` | the code pane |
| `prboom findings < list` | the walk's findings, one `FILE:LINE title` per line |
| `prboom comment FILE:LINE TEXT` | a note for the author; `prboom comments [--json\|--clear]` |
| `pr-pane CMD...` | runs a command in the window's Shell pane |
| `pr-close N` | close one: session, worktree, branch |
| `pr-close --stale` | close every PR that has since merged or closed |
| `pr-close --list` | what is open locally, and its PR state |
| `prdiff [BASE] [FILE]` | the whole PR diff, standalone |
| `skills/pr-walk` | what the agent follows |

The code pane tints changed lines rather than filling them with colour, so a
diff reads as code; a file the PR adds is shown as plain source, since every line
of it is new.

## Install

```
brew install bborn/prboom/prboom
prboom --link-skill
```

or

```
curl -fsSL https://raw.githubusercontent.com/bborn/prboom/main/install.sh | sh
```

Either way it is prebuilt, so Go is not needed. Homebrew must not write to your
home directory, so `--link-skill` is a separate step there; the curl script does
it for you.

The curl script is Files land in `~/.local/share/prboom`, commands
link into `~/.local/bin`, and the skill links into every `~/.claude*` config dir
it finds. Set `PRBOOM_PREFIX`, `PRBOOM_BIN` or `PRBOOM_VERSION` to change any of
that. Re-running it upgrades in place.

Working on prboom itself, `make install` symlinks straight out of the checkout
so edits are live.

## Using another agent

```
prboom --agent codex
pr-open --agent codex 3653
PR_AGENT=codex          # in ~/.config/prboom/config, to make it the default
```

Both paths take it. `pr-task` passes it to TaskYou as `--executor`, which speaks
the same names.

Only Claude Code has a skills directory to install into, so it gets `/pr-walk`
as a slash command. Every other agent is handed the same file to read, which
needs nothing installed on its side. Resuming a reopened PR uses `--continue`
for claude and `resume --last` for codex; an agent with neither just starts
again.

## Cleaning up

You do not have to. `prboom` sweeps on launch, in the background: any PR whose
session or worktree is still lying around, but which has since merged or closed,
gets its session killed and its worktree, branch and ref removed. One `gh` call
covers every PR at once, so it costs nothing whether you have one open or twenty.

A worktree with uncommitted changes is never removed. That is work you did by
hand and nothing else knows about it.

The sweep only ever sees what `pr-open` made: a session named `pr-<n>` and a
worktree under `PR_WORKTREE_ROOT`. TaskYou's worktrees live in
`.task-worktrees/<id>-<slug>` and its windows are named `task-<id>`, so nothing
here can match them. Those stay TaskYou's to manage.

## Making it yours

Nothing here needs editing to fit your setup. Two conventions cover it.

**`~/.config/prboom/config`** is plain shell, sourced before anything runs:

| | | |
|---|---|---|
| `PR_WORKTREE_ROOT` | where PR worktrees go | `~/.prboom/worktrees` |
| `PR_AGENT` | the coding agent | `claude` |
| `PR_SKILL` | slash command, for agents that have one | `/pr-walk` |
| `PR_SKILL_FILE` | the skill as a file, for agents that don't | in this repo |

**`~/.config/prboom/rules.md`** is prose the skill reads before it starts, and
it wins wherever it disagrees with the skill. This is where your test command,
your linter, and your taste in review comments go, so the skill itself stays
generic. A repo can add `.prboom.md` at its root for project-specific rules on
top.

Neither file has to exist.

### Teaching it how you review

```
L                  # in the picker
/pr-learn          # in Claude Code, inside the repo
pr-learn --all     # just the gathering step, re-reading everything
```

`pr-learn` reads PRs other people wrote in this repo that you reviewed or
commented on: your line comments with the code under them, your PR comments,
and the small commits you pushed to their branch. It does not read them all:
it scans the recent ones and keeps the 40 with the most of you in them (`--top`
changes that). The `pr-learn` skill reads
that, proposes rules with the quotes behind each, and asks you to keep, drop or
sharpen them, a few at a time.

What you keep goes in `~/.config/prboom/repos/OWNER/NAME.md`, which pr-walk
loads on top of the other two files, and which wins over both. Rules stay per
repo, because what you flag in one codebase is not what you flag in another.
Run it again later and it reads only what is new since the last time.

Comments an agent posted under your name are dropped where they are obvious and
ignored by the skill where they are not, so it learns your taste rather than
the agent's.

## Needs

`git` and `tmux`. `gh` authenticated, and `jq`. `delta` for the picker's diff
tab and `prdiff`; without it the picker falls back to plain colour. The code
pane highlights by itself.
`ty` only for the `t` path.
