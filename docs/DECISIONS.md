# Decisions

Why huddle works the way it does. [`SPEC.md`](SPEC.md) holds the
current rules; this file holds the reasons. One entry per decision, newest
last. When a decision is replaced, edit its entry; git history keeps the
old text.

## Profiles instead of one mixed view

**Decision:** a profile is one named world (sources, GitHub account,
cache, state), and only one shows at a time.
**Why:** work (ClickUp) and personal / open-source (GitHub issues) should
not mix, and the config should stay short.

## Each source keeps its own status words

**Decision:** no shared "stages" that every source maps into.
**Why:** a mapping table is one more thing to keep in sync, and ClickUp
and GitHub statuses don't line up well.

## GitHub split by org, not by repo list

**Decision:** a profile with `github_orgs` gets issues and PRs in those
orgs; a profile without gets everything else.
**Why:** repo lists go stale; orgs rarely change.

## A token per profile

**Decision:** with `github_account`, huddle reads that account's token
once and passes it as `GH_TOKEN` to its own `gh` calls.
**Why:** `gh auth switch` in another tab must not change what a profile
shows, and each profile can use a different account.

## ClickUp auto-detect

**Decision:** the workspace and the lead field are found at start and
cached; set values always win.
**Why:** a new user should not have to dig up IDs, and a normal start
should make no extra calls.

## One list tab per source; Today shows the main source only

**Decision:** ClickUp → Tasks, GitHub issues → Issues. Today shows only
the first source.
**Why:** at work every task is a ClickUp task tied to a PR, and issues
are extra notes. Mixing them into Today was noise.

## No meaning by blue, yellow, magenta or cyan alone

**Decision:** the active tab and filter are bold, inactive ones faint;
the cursor row has a grey (ANSI 8) background.
**Why:** some themes draw these colors as grey, so a blue active tab
looked like the inactive ones and a bold-only cursor was hard to see.

## Start panel: one column, title first

**Decision:** the title gets the full width at the top; fields follow
one per label line.
**Why:** an earlier, denser layout was too busy, and long repo or session
names squeezed the title.

## Checkout is the default workspace

**Decision:** the Start panel picks the repo checkout unless the task
looks like research.
**Why:** most tasks are worked on in the existing checkout; worktrees
are the exception.

## Agent state from `rex events --all`

**Decision:** one `rex events --all --json` stream plus `program_status`
at start.
**Why:** plain `rex events --json` has no status events for other
sessions.

## Flash messages without timers

**Decision:** a message stays until the next key press.
**Why:** timed messages disappear before you read them, and timers make
tests flaky.

## Paged PR diffs

**Decision:** when `gh pr diff` fails on a big PR, page each file's
patch from the PR files API.
**Why:** GitHub refuses diffs over 20000 lines (HTTP 406).
