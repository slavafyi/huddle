# huddle

A terminal UI, run in a Rex tab, for working on ClickUp tasks and GitHub
issues with their PRs, and for starting a coding agent on a task in one step.

**[Try the demo](https://slavafyi.github.io/huddle/)** (in the browser, with made-up data)

> [!NOTE]
> **Personal project.** huddle is built for my own workflow, with only the
> features I use. The code is open source in case it's useful to someone,
> but I can't promise to accept feature requests or pull requests. If you
> want it to work differently, fork it and make it yours.

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

- macOS. Linux and Windows builds are released too, but they are untested.
- Rex (with Remote Control on to focus agent tabs)
- `gh`, logged in
- A ClickUp API token for profiles with `clickup = true`: `CLICKUP_API_TOKEN`
  or, on macOS, the keychain item `huddle-clickup`

## Install

```sh
brew install slavafyi/tap/huddle
```

Or download an archive from
[Releases](https://github.com/slavafyi/huddle/releases), or build from source
with Go 1.26:

```sh
go install github.com/slavafyi/huddle/cmd/huddle@latest
```

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
   huddle            # last used profile
   huddle -p work    # a given profile
   ```

Press `?` in the app for keys.

## Development

Needs Go 1.26.

```sh
go run ./cmd/huddle              # run from source, inside a Rex tab
go test ./...                    # tests
go vet ./... && gofmt -l .       # checks; gofmt must print nothing
```

### Build

```sh
go build -o bin/huddle ./cmd/huddle    # this machine

# every platform into dist/, as a release would (nothing is published)
go run github.com/goreleaser/goreleaser/v2@v2.20.1 release --snapshot --clean
```

### Release

The version is the git tag (`vX.Y.Z`). Pushing a tag starts the release
workflow: it runs the tests, builds archives for every platform, creates
the GitHub release with notes from the commits, and updates the Homebrew
cask in [slavafyi/homebrew-tap](https://github.com/slavafyi/homebrew-tap).

```sh
git tag -s v1.0.1 -m v1.0.1
git push origin main v1.0.1
```

Use a patch bump for fixes, minor for features, major for breaking
changes. The workflow needs the `HOMEBREW_TAP_GITHUB_TOKEN` secret: a
fine-grained token with Contents read and write on the tap.

## Documentation

- [`docs/SPEC.md`](docs/SPEC.md) - behavior and config
- [`docs/DECISIONS.md`](docs/DECISIONS.md) - why things work the way they do
- [Demo](https://slavafyi.github.io/huddle/) - look, layout and keys

## How it was built

huddle is fully coded by AI, with Claude Code. I write the spec, make the
design decisions and use it every day; the code itself is written by the
agent.

## License

MIT License - see [LICENSE](LICENSE) for details.
