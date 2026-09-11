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
| `t` | same, but tracked as a TaskYou task |
| `d` | read the diff in delta, open nothing |
| `o` | open it on GitHub |
| `a` | toggle between yours and every open PR |
| `r` | refresh |
| `q` | quit |

The picker renders inline rather than in an alternate screen, so it leaves your
scrollback alone. It does not judge the PRs for you.

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
| `prdiff [BASE] [FILE]` | the whole PR diff, standalone |
| `skills/pr-walk` | what the agent follows |

`pr-show` renders by what is useful rather than what is technically a diff: the
hunks near your line for a changed file, syntax-highlighted source for a file the
PR adds, source centred on the line for one it doesn't touch. Delta's stock
styles are replaced with a dark tint so a diff reads as code rather than a block
of green; override with `PR_DELTA_OPTS`.

## Install

```
make install
```

Symlinks the binary and scripts into `~/bin` and the skill into every
`~/.claude*` config dir, so edits in this repo are live immediately.

## Needs

`git` and `tmux`. `gh` authenticated, and `jq`. `delta` and `bat` if you want
syntax highlighting; without them it falls back to `git diff --color` and `nl`.
`ty` only for the `t` path.
