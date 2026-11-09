# huddle — spec

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

**GitHub issues** (source `github`, on in every profile unless
`github_issues = false`): open issues assigned to me or opened by me, plus
those closed in the last 7 days, in the profile's org scope (§5). For
GitHub, "lead" means **opened by me**. Shown as `repo#42` (internal ID
`owner/repo#42`, unique ref `github:owner/repo#42`); the group column is the
repo; priority from labels `priority:high` / `priority:urgent` (same `!` /
`!!` marks); statuses `open` / `closed`. One `gh api graphql` search per
refresh (first 100 per query); incremental refreshes use
`updated:>=<since>`.

### 4.2 Today groups

| Group | Rule |
| --- | --- |
| In progress | assigned to me, `in progress` or `feedbacks` |
| Up next | assigned to me, `to start`, by due date |
| Waiting on others | I'm lead, not assignee, `to review` / `pending` / `to publish` |
| Done this week | closed in the last 7 days |

The status words above are ClickUp's: each task source supplies its own
(`task.Rules`).

GitHub issues use their own groups:

| Group | Rule |
| --- | --- |
| In progress | has a live agent tab |
| In review | has a linked open PR (§4.6) |
| Open | assigned to me, or opened by me with nobody assigned |
| Done this week | closed in the last 7 days |

A profile with both sources shows the union of groups in this order: In
progress, In review, Up next, Open, Waiting on others, Done this week. The
summary line counts each shown group.

**Tasks filters** (`t`): all, assigned to me, lead and not assigned to me.
Labels: ClickUp "Mine + Lead / Assigned / Lead only"; GitHub "All /
Assigned / Opened by me"; both sources "All / Assigned / Lead / opened".
ClickUp without a lead field (§5) has only "Assigned".

### 4.3 Task actions

- Change status (options from the API), comment, assign/unassign me, open in
  browser, copy branch name.
- Writes show at once, then go to the task's source. On failure, undo and
  show the error.
- GitHub issues: status `closed` / `open` runs `gh issue close` /
  `gh issue reopen`, comment `gh issue comment --body`, assign
  `gh issue edit --add-assignee @me` (`--remove-assignee`), each with
  `-R owner/repo`.

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

GitHub issues: the repo is known, so there is no repo picker. huddle finds
the local clone among `repo_roots` whose `origin` remote is the issue's
`owner/repo` (https or ssh form; read from `.git/config` when the repo list
is scanned). Without one the panel says `no local clone of owner/repo in
repo_roots`, and worktree / checkout launches stop with that message
(scratch still works). Branch `feature/{number}-{slug}`, prompt
`Take issue {full_id}` (`{full_id}` = `owner/repo#42`), no "Move to" row,
Rex session named after the repo. The `branch_template` and
`prompt_template` settings apply to ClickUp tasks only. Folder names use the
ID with unsafe characters as `-` (`shop#42` → `shop-42`).

On launch, in this order (stop at the first error, no rollback):

1. Folder: worktree from `origin/<base>` at `<worktree_root>/<repo>/{id_lower}`
   (reuse the branch or folder if it exists); checkout as is; or `mkdir`
   `<scratch_root>/{id_lower}-{slug}`.
2. Rex: new tab `{id} <title>` in the client's session (one session per
   client, named after the ClickUp list, found by stored ID, else by
   name). Create the session if missing. The tab runs the agent with the prompt.
3. Save the agent record; update ClickUp; remember the repo for this client.

### 4.5 Agents

Tabs huddle started, with state from Rex program status (Claude Code already
reports it): working, blocked (needs input), done, error, idle.

- Current state on start: `rex block call -b <block> program_status '{}'`.
- Changes: one `rex events --all --json` stream, filtered to
  `block_event` / `program_status_changed` for known block IDs. (The stream
  without `--all` has no status events — checked 2026-11-09.)

Focus or close a tab; offer to remove worktrees whose PR is merged (ask
first).

### 4.6 PRs

My open PRs and review requests in the profile's `github_orgs` (all of
GitHub when it has none), from `gh`. A PR's task is
the first known task ID (from the cache, any case, whole word) in its branch
name, else in its title. Also match ClickUp's own `CU-<clickup id>` form.
GitHub issues link first by the PR's closing references
(`closingIssuesReferences`, e.g. "Fixes #42"), else by the issue number as
a branch word (`42` or `42-…`) when the PR is in the issue's repo. No
match → no task.

### 4.7 Cache

Show cache first, refresh in the background: tasks every 3 min and after
writes (incremental with `date_updated_gt`, full every 30 min); PRs every
90 s; repo list daily. Agents are live, not cached. When offline, show cached
data and say so. A full refresh of one task source replaces only that
source's tasks; a source that fails keeps its cached tasks.

### 4.8 Colors

Only the 16 ANSI colors plus bold / faint / reverse, so the terminal theme
(light or dark) decides the real colors. No hex or 256 colors. Status colors
go by status name; unknown names are green when done, else faint.

## 5. Config and files

- Config: `~/.config/huddle/config.toml`. Shared settings are top-level:
  repo roots, worktree root, scratch root, branch and prompt templates,
  agent commands (`[agents]`), per-repo base branch (`[repos.base_branch]`).
- **Profiles:** every other top-level table is a profile, in file order
  (`agents` and `repos` are reserved names). A profile is one named world:
  its sources, GitHub account, cache and state. Only one is shown at a time.

  ```toml
  [work]
  clickup           = true               # this profile shows ClickUp tasks
  github_account    = "slavafyi"         # optional
  github_orgs       = ["vertexdigital"]  # GitHub issues and PRs in these orgs
  # github_issues   = false              # turn the GitHub issues source off
  # found automatically; set only to override:
  # clickup_workspace = "1234567"
  # clickup_lead      = "11111111-…"     # the "Lead Assignee" field

  [personal]
  github_account = "slavafyi"
  ```

  **GitHub split:** a profile with `github_orgs` searches issues and PRs
  only in those orgs (`org:X`); a profile without gets everything else
  (`-org:X` for every org another profile claims).
  **`github_account`:** huddle reads that account's token once at start
  (`gh auth token --user NAME`; failure stops with a message) and passes it
  as `GH_TOKEN` to its own `gh` calls only, so `gh auth switch` elsewhere
  doesn't matter. Without it, gh's active account is used. The token is
  never stored or logged.
  **`github_issues`:** on unless set to `false`.

  **ClickUp auto-detect:** a setting left out is found at start. The
  workspace is the only one the token sees (`GET /team`); several →
  error naming them (ID and name) and asking for `clickup_workspace`;
  none → error. The lead field is the one `users` custom field of that
  workspace whose name contains "lead", any case
  (`GET /team/{id}/field`); several → error listing them and asking for
  `clickup_lead`; none → lead features off (no "Waiting on others", only
  the "Assigned" filter, no lead query) with a one-time faint note.
  Found IDs are kept in `~/.cache/huddle/<profile>/clickup.json`, so a
  normal start makes no extra calls; a missing or unreadable file means
  detect again. When a task fetch gets a 4xx answer (not 429), huddle
  detects again; if the IDs changed it saves them, retries once and shows
  a yellow warning in the flash line. Set values always win and are never
  detected or replaced.

  A profile may override `repo_roots`, `scratch_root` and
  `prompt_template`. Unknown settings, no profiles, or a profile name
  other than letters, digits, `-` and `_` are errors at start.
- **Which profile:** `huddle -p <name>` (an unknown name lists the valid
  ones); else the last used one (`~/.local/state/huddle/last_profile`);
  else the first in the file. `P` opens a picker (most recently used first,
  from `recent_profiles` next to `last_profile`): type to filter, `enter`
  switches, `1–9` jump, `esc` closes. Switching ends the program and starts
  it again in the new profile. The header shows `huddle · <profile>`, the
  window title `Huddle · <profile>`. A profile without sources shows "no
  sources in this profile" in Today and Tasks; PRs still work.
- Per profile: cache `~/.cache/huddle/<profile>/cache.json`, state (agent
  records, client → session, client → recent repos)
  `~/.local/state/huddle/<profile>/state.json`.
- Write JSON atomically (temp file + rename).
- **Token:** env `CLICKUP_API_TOKEN`, else keychain item `huddle-clickup`.
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
go run ./cmd/huddle      # run inside a Rex tab
go test ./...            # must pass before commit
go vet ./... && gofmt -l .   # must be clean / print nothing
```

## 8. Structure

```
cmd/huddle/          main
internal/task/       source-neutral task, Source interface, rules (§4.1–4.2)
internal/clickup/    API client + ClickUp as a task Source
internal/github/     gh calls, PR → task link, issues source
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
  close Rex tabs huddle didn't create.

## 11. Git

Branches `feature/<name>`. Conventional Commits, short subject, no body.
Each commit builds and passes tests.

## 12. Phases

One fresh agent session per phase.

| # | Phase | Done when | Status |
| --- | --- | --- | --- |
| 0 | Checks: the two open questions in §13 | answers in §13 | ✓ 2026-11-09 |
| 1 | Skeleton: config, token, empty 4-tab app | tabs switch, `q` quits | ✓ |
| 2 | Read tasks + cache + Tasks view | real tasks show | ✓ |
| 3 | Status / comment / assign | writes work, undo on error | ✓ (tested with a fake ClickUp only) |
| 4 | Today view | §4.2 tests pass | ✓ |
| 5 | Start panel + folders | launch-step tests pass | ✓ |
| 6 | Rex launch + focus existing tab | 2nd `enter` only focuses | ✓ (focus tested with a fake Rex) |
| 7 | Agents view | live states show | ✓ |
| 8 | PRs view | link tests pass | ✓ |

Live checks (read-only, or throwaway Rex sessions) run with
`HUDDLE_LIVE=1`, `HUDDLE_REX_LIVE=1` or `HUDDLE_GH_LIVE=1`; see the
`live_test.go` files.

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

## 14. Profiles and GitHub issues (done; steps 1–4 built)

All of this is built. The main sections (§4, §5) hold the current
behavior and win where this plan differs; this section stays as the
record of the decision.

Decided 2026-11-10.

**Goal:** huddle works for work (ClickUp) and personal / open-source
(GitHub issues) without mixing them, with a short config.

### 14.1 Profiles

Built: see §5.

- A profile is one named world: its sources, GitHub account, cache and
  Rex sessions. Only one profile is shown at a time.
- The header shows the current one after the title: `huddle · work`. The
  Rex tab title is `Huddle · work`.
- `P` opens a picker: type to filter, `enter` switches, `1–9` jump, most
  recently used first. `huddle -p personal` starts in a profile; without
  `-p`, the last used one.
- Each source keeps its own status words. No shared "stages" to map.

### 14.2 Config

```toml
[work]
github_account = "slavafyi"         # optional, default: gh's active account
github_orgs    = ["vertexdigital"]  # GitHub items in these orgs belong here
# found automatically; set only to override:
# clickup_workspace = "1234567"
# clickup_lead      = "11111111-…"

[personal]
github_account = "slavafyi"
```

- A profile with `clickup = true` shows ClickUp tasks.
- **GitHub split:** issues and PRs in a profile's `github_orgs` belong to
  that profile. A profile without `github_orgs` gets every other issue
  and PR assigned to or opened by its account (any repo, open-source too).
  No repo lists.
- **`github_account`:** huddle calls `gh` with that account's token
  (`gh auth token --user NAME`), so `gh auth switch` elsewhere doesn't
  change what a profile shows. Each profile can use a different account.
- **ClickUp auto-detect** (checked 2026-11-10): the workspace is the only
  one the token can see (`GET /team`); the lead field is the one "users"
  custom field whose name contains "lead" (`GET /team/{id}/field`). None
  found → lead features off. Several → ask to set it. Cached after the
  first start; a warning if it stops matching.

### 14.3 GitHub issues in the views

Built: see §4.1–§4.6 (decided details there override this sketch).

- ID `repo#42`; the group column is the repo; priority from labels
  (`priority:high`, `priority:urgent`) with the same `!` / `!!` marks.
- Today: **In progress** (has an agent tab or my open PR), **Open**,
  **In review** (open PR waiting), **Done this week** (closed ≤ 7 days).
- Tasks: filters **Assigned** / **Opened by me**.
- Start panel: the repo is known, so no repo picker; huddle finds the
  local clone by its git remote. Branch `feature/{number}-{slug}`, Rex
  session named after the repo, no "Move to" row.
- PRs link to issues by "Fixes #42" or the issue number in the branch.

### 14.4 Build order

1. **Built.** Common item type; ClickUp behind a source interface (no
   visible change; existing tests stay green). Tasks and agent records are
   keyed by source + ID (state file version 2; older agents are ClickUp).
2. **Built.** Profiles in config, header, picker, `-p`; per-profile cache
   and state (§5). `github_account` is used from step 3.
3. **Built.** GitHub issues source, `github_account` token and org split
   for issues and PRs (§4.1–§4.6, §5). Cache version 3.
4. **Built.** ClickUp auto-detect for workspace and lead field (§5).
