# prboom

Pick the pull request worth your time, then get to work on it.

Run it inside any git repo. It lists open PRs with a verdict on each, you arrow
to one, and Enter checks it out.

```
prboom          # PRs waiting on your review
prboom -a       # every open PR
prboom -l       # print the list and exit, no picker
prboom -R owner/name
```

## Verdicts

Nothing here judges code quality. It decides what deserves your eyes, using the
cheap static cues that predict review effort.

| Verdict | Means |
|---|---|
| `STANDARD` | Normal. Open it. |
| `MECHANICAL` | Only lockfiles, generated code, vendored paths. Skim. |
| `HEAVY` | Over 1500 additions or 40 files. Needs a cut pass before a read. |
| `NEEDS-PLAN` | No description worth reading. Ask for one first. |
| `BLOCKED` | CI is red. Bounce it. |

## Keys

| Key | Does |
|---|---|
| `↑` `↓` `j` `k` | move |
| `⏎` | check out the PR and write its brief, via `pr-start` |
| `d` | show the PR's diff in delta, without checking it out |
| `o` | open it on GitHub |
| `a` | toggle between yours and every open PR |
| `r` | refresh |
| `q` | quit |

The list renders inline rather than in an alternate screen, so it leaves your
scrollback alone.

## Needs

`gh`, authenticated. `delta` for `d`. `pr-start` for `⏎`.

## Part of

The iTerm2 PR Review workgroup, set up in `~/.config/pr-review/SETUP.md`.
Enter hands off to `pr-start`, which records the merge base that the Diff peer
and `pr-goto` diff against.
