# prboom

Pick a pull request, hand it to TaskYou, walk it one item at a time.

```
prboom          # PRs waiting on your review
prboom -a       # every open PR
prboom -l       # print the list and exit, no picker
prboom -R owner/name
```

Enter runs `pr-task`, which creates a TaskYou task on that PR's branch so
TaskYou builds the worktree and starts an agent. Open the task and type
`/pr-walk` in the agent pane.

## Keys

| Key | Does |
|---|---|
| `↑` `↓` `j` `k` | move |
| `⏎` | make a TaskYou review task for this PR |
| `d` | read the diff in delta, no task |
| `o` | open it on GitHub |
| `a` | toggle between yours and every open PR |
| `r` | refresh |
| `q` | quit |

Renders inline rather than in an alternate screen, so it leaves your scrollback
alone. It does not judge the PRs for you; that was a triage step that earned its
own deletion.

## The rest of the pieces

| | |
|---|---|
| `pr-task N` | the non-interactive path: make the task for one PR |
| `/pr-walk` | the skill the agent runs, one finding at a time |
| `pr-show FILE[:LINE]` | renders one piece of code, never pages |
| `pr-pane CMD...` | runs a command in the task's Shell pane |

`/pr-walk` drives the pane itself: it runs `pr-pane pr-show <file>:<line>` so the
code is up before it says anything about it. `pr-show` never pages, so your
keyboard stays with the agent and a decision never costs a focus hop.

## Needs

`gh` authenticated, `jq`, `ty`, `delta`, `bat`.
