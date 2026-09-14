# prboom

Pick a pull request and walk it one finding at a time, with the relevant code in
the pane next to you.

```
prboom          # PRs waiting on your review
prboom -a       # every open PR
prboom -l       # print the list and exit, no picker
prboom -R owner/name
```

| Key | Does |
|---|---|
| `↑` `↓` `j` `k` | move |
| `⏎` | walk it now: worktree + tmux window + agent |
| `t` | same, but tracked as a TaskYou task, and opened in ty; reopens the task if there already is one |
| `d` | read the diff in delta, open nothing |
| `o` | open it on GitHub |
| `a` | toggle between yours and every open PR |
| `r` | refresh |
| `q` | quit |

The picker renders inline rather than in an alternate screen, so it leaves your
scrollback alone. It does not judge the PRs for you.

A PR you are already walking is marked beside its number: `ty 5421` for an
unfinished TaskYou review task in this repo's project, `open` for a `pr-open`
worktree.

## The walkthrough

Enter gives you two panes: the agent on the left, a Shell pane on the right. The
agent loads the `pr-walk` skill and, for each finding, puts the code in the right
pane *before* saying anything about it, then says four things:

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

## Two ways in, same shape

`⏎` runs `pr-open`, which needs **only git and tmux**. `t` runs `pr-task`, which
builds the same two panes through TaskYou and puts a card on the board. The skill
works under either because it just looks for a pane titled `Shell`.

## Pieces

| | |
|---|---|
| `prboom` | the picker |
| `pr-open N` | worktree + tmux window + agent, no other dependencies |
| `pr-task N` | the same via TaskYou, tracked on the board |
| `pr-show FILE:LINE [CTX]` | renders one piece of code, never pages |
| `pr-pane CMD...` | runs a command in the window's Shell pane |
| `pr-close N` | close one: session, worktree, branch |
| `pr-close --stale` | close every PR that has since merged or closed |
| `pr-close --list` | what is open locally, and its PR state |
| `prdiff [BASE] [FILE]` | the whole PR diff, standalone |
| `skills/pr-walk` | what the agent follows |

`pr-show` renders by what is useful rather than what is technically a diff: the
hunks near your line for a changed file, syntax-highlighted source for a file the
PR adds, source centred on the line for one it doesn't touch. Delta's stock
styles are replaced with a dark tint so a diff reads as code rather than a block
of green; override with `PR_DELTA_OPTS`.

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
| `PR_DELTA_OPTS` | extra flags for delta | none |

**`~/.config/prboom/rules.md`** is prose the skill reads before it starts, and
it wins wherever it disagrees with the skill. This is where your test command,
your linter, and your taste in review comments go, so the skill itself stays
generic. A repo can add `.prboom.md` at its root for project-specific rules on
top.

Neither file has to exist.

## Needs

`git` and `tmux`. `gh` authenticated, and `jq`. `delta` and `bat` if you want
syntax highlighting; without them it falls back to `git diff --color` and `nl`.
`ty` only for the `t` path.
