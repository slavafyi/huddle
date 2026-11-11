# huddle

A terminal UI, run in a Rex tab, for working on ClickUp tasks and GitHub
issues with their PRs, and for starting a coding agent on a task in one step.

## What it does

- Today view: in progress, up next, waiting on others, done this week
- Task and issue lists with status, comment and assign actions
- Start panel: pick a repo checkout, worktree or scratch folder and an agent
  (Claude Code, Codex, pi, opencode, Cursor, or your own), then launch it in a
  new Rex tab
- PRs view with CI state, linked to tasks and issues
- Agents view with live state from Rex
- Profiles: separate worlds (e.g. work ClickUp and personal GitHub), each with
  its own GitHub account, cache and state

## Requirements

- macOS, Go 1.26
- Rex (with Remote Control on to focus agent tabs)
- `gh`, logged in
- A ClickUp API token for profiles with `clickup = true`: `CLICKUP_API_TOKEN`
  or the keychain item `huddle-clickup`

## Getting started

1. Create `~/.config/huddle/config.toml` with at least one profile:

   ```toml
   #:schema file:///path/to/huddle/config.schema.json

   [work]
   clickup     = true
   github_orgs = ["my-org"]
   repo_roots  = ["~/code/job"]

   [personal]
   repo_roots = ["~/code/home"]
   ```

   [`config.schema.json`](config.schema.json) gives checks and completion in
   tombi, Taplo or Even Better TOML. [`docs/SPEC.md`](docs/SPEC.md) §5 lists
   every setting.

2. Run it inside a Rex tab:

   ```sh
   go run ./cmd/huddle            # last used profile
   go run ./cmd/huddle -p work    # a given profile
   ```

Press `?` in the app for keys.

## Common commands

- `go run ./cmd/huddle` - run the app
- `go build -o bin/huddle ./cmd/huddle` - build a binary
- `go test ./...` - run the tests
- `go vet ./... && gofmt -l .` - check the code

## Documentation

- [`docs/SPEC.md`](docs/SPEC.md) - behavior and config
- [`docs/DECISIONS.md`](docs/DECISIONS.md) - why things work the way they do
- [`demo/index.html`](demo/index.html) - look, layout and keys

## License

MIT License - see [LICENSE](LICENSE) for details.
