# cuddle — spec

A terminal UI, run in a Rex tab, for working on ClickUp tasks and their
GitHub PRs, and for starting a coding agent on a task in one step.

This spec covers rules and behavior.

## 1. Objective

- **User:** one developer (the owner) doing ClickUp tasks for many Shopify
  clients, mostly with Claude Code in Rex.
- **Problem:** starting a task takes many manual steps (find task, pick repo,
  name branch, open Rex, start Claude, paste ID, change status), and task, PR
  and agent state live in three places.
- **Success:**
  - The task list shows in under 300 ms from cache.
  - `enter`, then `ctrl-s` → Claude runs on the task in the right folder.
  - One screen shows: doing now, next, waiting on others, done.
  - The UI never freezes on network calls.

## 2. Scope

**v1:** Today, Tasks (status / comment / assign), Start panel, Agents, PRs,
background refresh with cache.

**Not v1:** creating or editing tasks, time tracking, other people's tasks,
merging or approving PRs, Linux/Windows.

## 3. Domain facts

Checked on 2026-11-09. They come from config or the API, not from code.

- Workspace `1234567`, one space **ON-DEMAND**, **one list per client**.
- **Task ID** (`{id}` below): the task's `custom_id` from the API (today
  `DEV-7808`), else its ClickUp ID (`86a1b2c3d`). The prefix is a ClickUp
  setting and may change, so **never hard-code `DEV`**. `{id_lower}` is
  the same in lowercase. Custom-ID API calls need
  `custom_task_ids=true&team_id=1234567`.
- Statuses: `to plan`, `to start`, `in progress`, `to review`, `feedbacks`,
  `pending`, `to publish`, `published`, `Closed` (case-sensitive).
- Field `🧑‍💻 Lead Assignee` (users): `11111111-2222-4333-8444-555555555555`.
- Task types: Coding, Idea, Design, Content, Email, Personal.
- GitHub org `vertexdigital`, via `gh`. ClickUp has no GitHub links today.
- Branch convention: `feature/dev-7725-free-can-physical-only`.
- Repos: `~/code/agency/{shops,tools}/*`. One client can have many repos.

## 4. Behavior

### 4.1 Which tasks

Open tasks where I'm assignee **or** Lead Assignee, plus tasks closed in the
last 7 days.

### 4.2 Today groups

| Group | Rule |
| --- | --- |
| In progress | assigned to me, `in progress` or `feedbacks` |
| Up next | assigned to me, `to start`, by due date |
| Waiting on others | I'm lead, not assignee, `to review` / `pending` / `to publish` |
| Done this week | closed in the last 7 days |

### 4.3 Task actions

- Change status (options from the API), comment, assign/unassign me, open in
  browser, copy branch name.
- Writes show at once, then go to ClickUp. On failure, undo and show the error.

### 4.4 Start a task

`enter` on a task: if its agent tab is still open, **focus it and stop**.
Otherwise open the Start panel (`shift+enter` always opens it).

Panel choices and defaults:

- **Workspace:** repo worktree (default), repo checkout, or scratch folder.
  Scratch is the default for type Idea or titles starting with Research,
  Evaluate, Investigate, Check, Audit.
- **Repo:** the last ones used for this client first.
- **Branch:** `feature/{id_lower}-{slug}` (slug: lowercase ASCII, up to 5
  words), e.g. `feature/dev-7700-promo-checkout-personalisation`.
- **Agent:** Claude (default), Codex, or shell only.
- **Prompt:** empty means `Take a task {id} from CU`.
- **Move to in progress** if the task is `to plan` / `to start`.
- **Assign to me**: only shown when I'm not an assignee.

On launch, in this order (stop at the first error, no rollback):

1. Folder: worktree from `origin/<base>` at `<worktree_root>/<repo>/{id_lower}`
   (reuse the branch or folder if it exists); checkout as is; or `mkdir`
   `<scratch_root>/{id_lower}-{slug}`.
2. Rex: new tab `{id} <title>` in the client's session (one session per
   client, named after the ClickUp list, found by stored ID). Create the
   session if missing. The tab runs the agent with the prompt.
3. Save the agent record; update ClickUp; remember the repo for this client.

### 4.5 Agents

Tabs cuddle started, with state from Rex program status (Claude Code already
reports it): working, blocked (needs input), done, error, idle.

- Current state on start: `rex block call -b <block> program_status '{}'`.
- Changes: one `rex events --all --json` stream, filtered to
  `block_event` / `program_status_changed` for known block IDs. (The stream
  without `--all` has no status events — checked 2026-11-09.)

Focus or close a tab; offer to remove worktrees whose PR is merged (ask
first).

### 4.6 PRs

My open PRs and review requests in `vertexdigital`, from `gh`. A PR's task is
the first known task ID (from the cache, any case, whole word) in its branch
name, else in its title. Also match ClickUp's own `CU-<clickup id>` form.
No match → no task.

### 4.7 Cache

Show cache first, refresh in the background: tasks every 3 min and after
writes (incremental with `date_updated_gt`, full every 30 min); PRs every
90 s; repo list daily. Agents are live, not cached. When offline, show cached
data and say so.

### 4.8 Colors

Only the 16 ANSI colors plus bold / faint / reverse, so the terminal theme
(light or dark) decides the real colors. No hex or 256 colors.

## 5. Config and files

- Config: `~/.config/cuddle/config.toml` (workspace ID, lead field ID, repo
  roots, worktree root, scratch root `~/code/agency/notes`, GitHub
  org, branch and prompt templates, agent commands, per-repo base branch).
- Cache: `~/.cache/cuddle/*.json`. State (agent records, client → session,
  client → recent repos): `~/.local/state/cuddle/state.json`.
- Write JSON atomically (temp file + rename).
- **Token:** env `CLICKUP_API_TOKEN`, else keychain item `cuddle-clickup`.
  Never stored in files or logs.
- **Base branch:** `origin/HEAD`, unless the config overrides it for a repo
  (e.g. `snowcap = "develop"`).

## 6. Tech stack

- Go 1.26.
- `charm.land/bubbletea/v2` v2.1, `charm.land/bubbles/v2` v2.2,
  `charm.land/lipgloss/v2` v2.0, `github.com/BurntSushi/toml`.
- ClickUp REST API v2 over `net/http`. GitHub via `gh`. Rex via the `rex` CLI
  (`--json`, `rex events --json`).

## 7. Commands

```sh
go run ./cmd/cuddle      # run inside a Rex tab
go test ./...            # must pass before commit
go vet ./... && gofmt -l .   # must be clean / print nothing
```

## 8. Structure

```
cmd/cuddle/          main
internal/clickup/    API client + task rules (§4.1–4.2, pure functions)
internal/github/     gh calls + PR → task link
internal/rex/        rex calls + events
internal/workspace/  repos, branch names, worktree/scratch
internal/store/      cache + state files
internal/ui/         Bubble Tea views
```

The UI never runs commands or HTTP itself. It calls the packages above
through interfaces, inside `tea.Cmd`.

## 9. Testing

- TDD for the rules in §4: grouping, branch slug, PR link, launch steps.
- ClickUp: `httptest` server with JSON fixtures. `git` / `gh` / `rex`: a fake
  runner; tests check the exact arguments.
- No test calls the real ClickUp, GitHub or Rex.

## 10. Boundaries

- ✅ **Always:** run tests, vet and gofmt before commit; keep network calls
  off the UI thread; at the end of a phase, list any requirement not met.
- ⚠️ **Ask first:** new dependencies; changing file formats; ClickUp writes
  other than status / assignee / comment; deleting worktrees or branches.
- 🚫 **Never:** log or commit the token; move a task to `to review` or later
  without a key press; `git push`, reset or switch branches in my checkout;
  close Rex tabs cuddle didn't create.

## 11. Git

Branches `feature/<name>`. Conventional Commits, short subject, no body.
Each commit builds and passes tests.

## 12. Phases

One fresh agent session per phase.

| # | Phase | Done when |
| --- | --- | --- |
| 0 | ~~Checks~~ — done 2026-11-09, see §13 | ✓ |
| 1 | Skeleton: config, token, empty 4-tab app | tabs switch, `q` quits |
| 2 | Read tasks + cache + Tasks view | real tasks show |
| 3 | Status / comment / assign | writes work, undo on error |
| 4 | Today view | §4.2 tests pass |
| 5 | Start panel + folders | launch-step tests pass |
| 6 | Rex launch + focus existing tab | 2nd `enter` only focuses |
| 7 | Agents view | live states show |
| 8 | PRs view | link tests pass |

## 13. Phase 0 results (2026-11-09)

1. **Agent status from every session:** `rex events --all --json` delivers
   `program_status_changed` for all sessions; plain `rex events --json` does
   not. Current state comes from the block method `program_status`.
2. **"Lead = me" filter:** `GET /team/{id}/task` accepts
   `custom_fields=[{"field_id":"<lead>","operator":"ANY","value":["<me>"]}]`
   (21 open tasks). "My tasks" = this query + `assignees[]=<me>`, merged by
   ID. `date_updated_gt` works for incremental refresh. Rate limit: 100
   requests/min.

Focusing a tab in the Mac app uses the `session.select` client action, so
Rex **Remote Control** must be on. If it's off, show a hint.
