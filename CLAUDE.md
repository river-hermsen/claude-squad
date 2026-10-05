# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Claude Squad (`cs`) is a Go TUI (Bubble Tea) that runs multiple AI agents (Claude Code, Codex, Gemini, Aider, …) side by side, each in its own tmux session and its own git worktree.

## Commands

```bash
go build -o cs .                       # build (binary names `cs` / `claude-squad` are gitignored)
go build -o ~/.local/bin/cs .          # install this fork as the user's daily `cs` (replaces the old brew install)
go test ./...                          # full suite — same as CI
go test ./session -run TestStartPausesInstance -v   # single test
gofmt -l .                             # CI fails if this prints anything; fix with `gofmt -w .`
```

- Some tests shell out to real `tmux` and `git` (e.g. `ui/preview_test.go`, `session/git/worktree_ops_test.go`), so both must be on PATH.
- CI lint also runs golangci-lint v1.60.1 with `--fast` and `only-new-issues`.
- Logs go to `$TMPDIR/claudesquad.log` (path is printed on exit).
- To drive the real TUI, run it in a detached tmux session and use `send-keys` / `capture-pane`. Set `HOME` *inside* the command (`tmux new-session -d ... "env HOME=<scratch> ./cs -p bash"`). New tmux sessions take the tmux server's environment, not the calling shell's, so an exported `HOME` is ignored and `cs` reads and writes the real `~/.claude-squad`.
  - The agent sessions `cs` starts get the server's environment too. So a real `claude` there stays logged in, while `cs` itself uses the scratch `HOME`. That makes end-to-end tests with real Claude (`-p "<abs path>/claude --model haiku --permission-mode acceptEdits --allowedTools Bash,Edit,Write,Read,Grep,Glob"`) cheap and safe in a scratch directory of repos.
- `cs debug` prints the config path and contents; `cs reset` wipes stored instances, `claudesquad_*` tmux sessions and worktrees.
- `clean.sh` / `clean_hard.sh` run `tmux kill-server` and `rm -rf ~/.claude-squad` — they kill **every** tmux session on the machine, not just Claude Squad's.
- Go: `go.mod` says 1.23.0 with toolchain 1.25.8; CI pins 1.25.8.

### Release

`./bump-version.sh` bumps the patch number of `version` in `main.go`, commits "Bump version to X", and creates tag `vX` (does not push). The release workflow rejects any tag that doesn't match `main.go`'s `version`, then GoReleaser cross-builds with `CGO_ENABLED=0` for linux/darwin/windows. The GitHub changelog drops commits prefixed `docs:`, `chore`, `refactor`.

### Website

`web/` is a separate Next.js site (`npm --prefix web run dev`), deployed to GitHub Pages on changes under `web/`. It is unrelated to the Go build.

## Architecture

Layering, top to bottom:

```
main.go (cobra)  →  app/ (Bubble Tea model `home`)  →  session.Instance  →  session/tmux.TmuxSession
                                                                          →  session/git.GitWorktree        (single repo)
                                                                          →  session/workspace.Workspace    (directory of repos)
```

### Instance lifecycle (`session/instance.go`)

- **Title is the identity.** It names the tmux session (`claudesquad_<title>`, whitespace stripped, `.`→`_`), the branch (`<config.BranchPrefix><title>`, default prefix `<username>/`), and is the storage key. It cannot change after `Start`.
- **`Start(true)`** (new instance): creates a worktree under `~/.claude-squad/worktrees/<branch>_<hex-nanos>`, then `tmux new-session -c <worktree> <program>`. If the user picked an existing branch, `isExistingBranch` is set and cleanup will not delete that branch.
- **Outside a git repo** (fork-specific), `Start(true)` picks one of two modes. At most one of `gitWorktree` / `workspace` is set. Gate on `HasWorktree()` / `IsMultiRepo()` / `InPlace()` and use `GetWorkDir()` and `IsBranchCheckedOut()` instead of reaching into either field. `home.inGitRepo` (from the launch directory) hides the branch picker and skips `git fetch` in both modes.
  - **Multi-repo**: the directory directly contains git repos (like `~/Projects/FOYS/foys-all`) and the program is Claude Code (`workspace.SupportsProgram`). See the workspace section below.
  - **In place**: anything else, including non-Claude agents in a directory of repos. tmux runs directly in `Path`, with no branch, diff or checkout (`c`). Several such sessions in one directory share it with no isolation. They persist with empty `worktree` data and no `workspace`.
- **`Start(false)`** (loaded from disk): reattaches to the existing tmux session. If the session is gone (`tmux.ErrSessionNotFound`), the instance is parked as `Paused` instead of erroring — `Storage.LoadInstances` aborts on the first error, so one dead session would hide all the others.
- **`Pause`**: commits dirty changes locally, detaches tmux, removes the worktree but keeps the branch, copies the branch name to the clipboard.
- **`Resume`**: recreates the worktree only if it is missing/invalid (so it doesn't discard uncommitted work), then restores or restarts tmux. Refuses if the branch is checked out elsewhere.
- **`Kill`**: closes tmux, then cleans up the worktree (and branch, unless pre-existing).

### Multi-repo workspaces (`session/workspace/`, fork-specific)

Claude runs in the real directory, so Read/Grep/Glob see every repo as usual. Each repo gets a git worktree only when Claude first changes it.

- **Launch**: `Instance.launchProgram()` appends `Workspace.ClaudeArgs()` to the program: `--settings` (a PreToolUse hook running `cs hook --workspace <dir>`), `--add-dir <dir>` and `--append-system-prompt-file`. Settings passed with `--settings` merge with the user's own hooks; nothing in their settings files is touched.
- **Hook** (`hook.go`, hidden `cs hook` command): it runs in Claude's process, not the TUI's.
  - Edit/Write/MultiEdit/NotebookEdit under `<root>/<repo>/` create the worktree (`Isolate`) and *deny* the call, with a message pointing at the same file in `<dir>/<repo>`. The deny is deliberate: silently rewriting the path would leave Claude reading stale files from the real checkout.
  - Bash creates worktrees only for commands `isRiskyCommand` flags (git writes, `sed -i`, `rm`/`mv`/`cp`, redirects, package installs, `dotnet ef`, formatters). Read-only commands and builds keep running on the real checkout, which has `node_modules`/`bin`.
  - Once a repo is isolated, any Read/Grep/Glob/Bash aimed at its real checkout is redirected.
  - The hook's stdout is Claude Code's response, so `cs hook` must print nothing else there (no `log.Close`).
- **State**: `<dir>/.claudesquad/repos.json` (repo name + base commit) is the source of truth. The hook writes it under a file lock with atomic renames, and the TUI re-reads it on every diff tick. Worktrees are created from each repo's current HEAD on branch `<prefix><title>`.
- **Diff/list**: `DiffStats.Repos` holds one `RepoDiff` per repo with changes; the aggregate `Content` stays empty. The diff tab shows one repo at a time under a selector bar. `←/→` (`KeyPrevRepo`/`KeyNextRepo`) switch repos, and the selection is kept by name across refreshes. The list shows `<dir> · <repo>` or `<dir> · N repos`.
- **Pause/kill**: these loop over the repos via the existing `GitWorktree` methods. `Cleanup` refuses to delete anything outside `~/.claude-squad/workspaces`. `cs reset` calls `workspace.CleanupAll`.
- **Facts verified against Claude Code 2.1.x**:
  - Grep/Glob do not descend into symlinked directories.
  - Edits through a symlink that resolves outside the project need extra permission.
  - `cd` into such a symlink is reset.
  - So workspaces must not symlink repos into a separate folder.
  - Claude's folder-trust dialog defaults to "No, exit", so blindly sending Enter kills the session.

### Persistence (`config/`, `session/storage.go`)

- `~/.claude-squad/config.json` → `config.Config` (default program, `auto_yes`, `daemon_poll_interval`, `branch_prefix`, `profiles`). Created with defaults on first load; `DefaultConfig` resolves the `claude` path via the user's shell.
- `~/.claude-squad/state.json` → `config.State`; instances are stored as raw JSON and decoded by `session.Storage`.
- Only started instances are saved. Adding a persisted field means updating `InstanceData` / `GitWorktreeData` / `WorkspaceData` **and** both `ToInstanceData` and `FromInstanceData`.

### Event loop and concurrency (`app/app.go`)

- `home.state` is a small state machine (`stateDefault`, `stateNew`, `statePrompt`, `stateHelp`, `stateConfirm`) that drives which overlay (`ui/overlay/`) and menu state are active.
- Expensive I/O stays off the Bubble Tea main loop:
  - `tickUpdateMetadataCmd` self-chains every 500ms. It checks every active instance in parallel goroutines (tmux pane hash for running vs ready, prompt detection, git diff). Only the selected instance gets a full diff; the others get `--numstat` counts to bound memory.
  - Results come back as `metadataUpdateDoneMsg` and are applied in `Update` on the main thread (`SetStatus`, `SetDiffStats`). Never mutate `Instance` state from the background goroutine.
  - `previewTickMsg` refreshes the preview pane every 100ms; new instances start in the background via `runInstanceStartCmd`.
- The right-hand `ui.TabbedWindow` has Preview (captured agent pane), Diff, and Terminal tabs. The Terminal tab keeps a separate shell tmux session per instance in its worktree.
- The Diff tab does not show raw `git diff`. `ui/diffview.go` parses it, counting hunk lines from the `@@` header, and renders file header bars, old/new line numbers, full-width tinted `+`/`-` rows and word-aware wrapping to the pane width. `DiffPane.setRaw` caches the rendering, because the diff refreshes every 100ms.
- `GlobalInstanceLimit = 10`.

### AutoYes and the daemon (`daemon/`)

With `-y` / `auto_yes`, `HasUpdated` prompt detection triggers `TapEnter`. When the TUI exits in AutoYes mode, it re-execs itself as `cs --daemon` (hidden flag; detached via `Setsid` on Unix) and writes `~/.claude-squad/daemon.pid`. The daemon keeps auto-accepting prompts across all instances. Every TUI start calls `daemon.StopDaemon()` first, so the TUI and daemon never drive the same sessions at once.

### Agent-specific behaviour (`session/tmux/tmux.go`)

Per-agent behaviour is keyed on the program string compared against `ProgramClaude` / `ProgramAider` / `ProgramGemini`, and the matching differs by call site:

- Trust-prompt dismissal (`CheckAndHandleTrustPrompt`) uses a suffix match.
- AutoYes prompt detection (`HasUpdated`) uses an exact match for claude and a prefix match for aider and gemini.

Both detect screens by literal UI strings. Support for a new agent, or a fix after an agent changes its prompt wording, goes here.

### Testability seams

- All external commands go through `cmd.Executor`; tmux PTYs go through `tmux.PtyFactory`. Tests inject fakes with `tmux.NewTmuxSessionWithDeps(...)`, `cmd_test.MockCmdExec`, and `instance.SetTmuxSession(...)`.
- The `log` package exposes global loggers that are nil until `log.Initialize`. Any test package touching code that logs needs a `TestMain` that calls `log.Initialize(false)` (see `app/app_test.go`, `session/instance_test.go`).
- Platform splits use `_unix.go` / `_windows.go` files (`daemon`, `session/tmux`). Keep Windows building: CI builds windows/amd64.

### Keybindings

`keys/keys.go` is the source of truth (`GlobalKeyStringsMap` + `GlobalkeyBindings`). This fork removed upstream's push key (`p`), along with all `gh` usage; pushing is left to the user or the agent. The bottom menu's `│` groups and highlighted action group come from `Menu.setGroups`, so build options as groups rather than a flat list.

## Contributing

PRs need the CLA signed (CLA Assistant bot). Recent commits use conventional prefixes (`fix:`, `chore:`).
