---
name: address-review
description: Read the human's inline review comments in a git worktree, act on each one, and reply in the thread. Use when the user says "address the review", "address my comments", "handle the review comments", "/address-review", or points at review threads in a worktree. Also use for an adversarial pass where you add your own review comments. Threads are the local pre-publication discussion created by the Git Worktree Diff extension.
---

# Address review

Set `SKILL_ROOT` to the absolute directory containing this `SKILL.md` before running any packaged helper. This contract is independent of the agent harness.

A worktree carries a local PR-style discussion at `<worktree>/.worktree-review/threads.json`:
inline comment threads anchored to file and line, written by the reviewer in the
editor and by agents through a CLI. Your job is to work each open thread and
**always reply** — whether you make the change, decline it, or only respond to a
thought.

The reviewer sees your replies inline in the editor, live.

## Rules

1. **Every thread you touch gets a reply.** Silence is a failure. Accepting,
   rejecting and "noted, here is the trade-off" all end in a reply.
2. **Never set `resolved`.** That is the reviewer's sign-off. You set
   `addressed`, `rejected` or `noted`.
3. **Write replies in Simplified Technical English.** Invoke the
   `simple-english` skill and apply it to every reply body, and to any English
   you add in code comments or commit messages during this pass. Short sentences,
   one idea each, active voice, condition before command.
4. **Change code only when the thread asks for it** and you agree. Make the edit
   first, then reply naming the files you touched.
5. **Say why when you decline.** A rejection with no reason is not a reply.
6. **Identify yourself.** Set `AGENT_HARNESS` and `AGENT_MODEL` to the active
   runtime and model, then pass both values with every reply or review comment.
7. **Do not rewrite the reviewer's comments** and do not delete threads.

## Locate the tooling

Use the helper source packaged with this skill:

```sh
WT=$(git rev-parse --show-toplevel)
REVIEW="$SKILL_ROOT/scripts/review.js"
node "$REVIEW" --dir "$WT" counts
```

Read `.worktree-review/threads.json` by hand only as a last resort. The CLI keeps IDs, timestamps, status transitions, and the rendered `DISCUSSION.md` consistent.

## Workflow

### 1. Read what is waiting

```sh
node "$REVIEW" --dir "$WT" list --awaiting --json
```

`--awaiting` gives the threads whose last comment is **not** from a bot, so those
need you. `list --open` gives every unresolved thread; `list --all` gives
everything including resolved ones.

For one thread with its full history:

```sh
node "$REVIEW" --dir "$WT" show t3 --json
```

### 2. Understand each comment in context

Read the file around `startLine`..`endLine` before you decide. The comment may
have drifted if the file changed; `anchorText` is the line the reviewer selected,
so search for it when the line number looks wrong.

Read the rest of the thread too. An earlier agent may have already answered, or
another reviewer may have pushed back on it.

### 3. Decide, act, reply

| Situation | Action | Status |
| --- | --- | --- |
| You agree and change the code | edit the files, then reply naming them | `addressed` |
| You agree but it belongs in separate work | reply with what you would do and why not now | `noted` |
| You disagree | reply with the reason and the evidence | `rejected` |
| You need a decision from the reviewer | reply with the question and the options | `noted` |
| The comment is a thought, not a request | reply with your view | `noted` |

Reply through stdin, so markdown and newlines survive:

```sh
node "$REVIEW" --dir "$WT" reply t3 \
  --agent "$AGENT_HARNESS" --model "$AGENT_MODEL" \
  --action accepted --status addressed --body - <<'EOF'
Changed `parseRemote` to strip the port from the host. A scheme URL now splits on
the first `/`, so `ssh://host:2222/o/r` gives host `host`.

Files: review-store.js
EOF
```

`--action` is the badge on your reply: `accepted`, `rejected`, `noted`,
`question`, or `review`. `--status` sets the thread status in the same call.

### 4. Verify before you claim it is done

Run whatever the repo uses — the project's tests, a linter, or a build. Quote the
result in the reply. Do not write "fixed" for a change you did not run.

### 5. Refresh the discussion

Every `reply`, `add` and `status` rewrites `.worktree-review/DISCUSSION.md`. Run
`node "$REVIEW" --dir "$WT" render` if you edited the store any other way.

## Adversarial pass

When the user asks for a review instead of a response to one, raise your own
threads. Several agents can review the same worktree at once; each comment
carries its author, so the thread shows who said what.

```sh
node "$REVIEW" --dir "$WT" add --file src/thing.js --line 42 --end 48 \
  --agent "$AGENT_HARNESS" --model "$AGENT_MODEL" --action review --body - <<'EOF'
This loop reads the file on every iteration. Move the read above the loop.
EOF
```

Guidance for an adversarial pass:

- Review the diff against the base branch, not the whole repo. Get it with
  `git diff $(git merge-base origin/main HEAD)`.
- One thread per problem, anchored to the line that has to change.
- State the failure, not a preference. "This throws when `files` is empty" beats
  "this could be cleaner".
- Do not raise a thread another agent already raised. Check `list --all` first.
- Say plainly when you find nothing, rather than padding the thread count.

## Reply style

Apply `simple-english` to the body. In practice:

- One idea per sentence, 20 words or fewer.
- Active voice: "I changed the parser", not "the parser was changed".
- Condition first: "If the remote has no port, the host stays unchanged."
- Name files and symbols in backticks, exactly as they are spelled.
- No apologies, no filler, no restating the reviewer's comment back at them.

Good:

> Agreed. `counts()` walked the threads twice. It now walks them once and
> returns both totals. Files: `review-store.js`. Harness passes.

Bad:

> Great catch! I've gone ahead and refactored this a bit — it should be much
> cleaner now, let me know what you think!

## Finish

After the pass, tell the user:

- how many threads you answered, and the split between addressed, rejected and noted;
- which files you changed;
- what you ran to verify;
- which threads still need a decision from them.

Never mark your own work `resolved`. Leave that to the reviewer.
