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
- `cs debug` prints the config path and contents; `cs reset` wipes stored instances, `claudesquad_*` tmux sessions, workspaces and `~/.claude-squad/sessions`.
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
                                                                          →  session/workspace.Workspace  →  session/git.GitWorktree (one per repo)
```

All git isolation goes through a `Workspace` in `~/.claude-squad/workspaces/<branch>_<hex-nanos>/`. This fork removed upstream's `~/.claude-squad/worktrees/` and the `NewGitWorktree*` constructors; `GitWorktree` is only the per-repo engine a workspace drives.

### Instance lifecycle (`session/instance.go`)

- **Title is the identity.** It names the tmux session (`claudesquad_<title>`, whitespace stripped, `.`→`_`), the branch (`<config.BranchPrefix><title>`, default prefix `<username>/`), and is the storage key.
  - `SetTitle` only works before `Start`. Afterwards, `Rename` (`R`) changes the title and renames the tmux session.
  - Claude sessions are launched with `--name <title>`. After a rename, `SyncClaudeName` (run on idle metadata ticks) sends `/rename <title>`. It waits until `claudePromptIsEmpty` sees the prompt box empty: a `❯` line between two `─` rules that holds only blanks or the dimmed placeholder. That way the command never lands in the middle of a draft.
  - The workspace directory and the Claude settings directory keep their original names. A running Claude still uses them, so the settings directory is stored as `InstanceData.ClaudeDir` rather than derived from the title.
  - The branch follows the new name (`Workspace.Rename`) only while no repository has a worktree. Hook processes see that through `isolate`, which re-reads `workspace.json` under the lock.
- **`Start(true)`** (new instance) picks a mode from the launch directory. Gate on `InPlace()` / `IsMultiRepo()` and use `GetWorkDir()` / `IsBranchCheckedOut()` rather than reaching into `workspace`. `home.inGitRepo` (from the launch directory) decides whether the branch picker and `git fetch` run.
  - **Single-repo** (launched inside a git repo, any agent): `workspace.NewSingle` isolates that repo up front in `<workspace>/<repo>`, on a new `<prefix><title>` branch or the branch picked in the picker (`Repo.ExistingBranch`, kept on cleanup). The agent runs inside that worktree, without a hook.
  - **Multi-repo** (fork-specific): the directory directly contains git repos (like `~/Projects/FOYS/foys-all`) and the program is Claude Code (`workspace.SupportsProgram`). See the workspace section below.
  - **In place**: anything else, including non-Claude agents in a directory of repos. tmux runs directly in `Path`, with no branch, diff or checkout (`c`). Several such sessions in one directory share it with no isolation.
- **`Start(false)`** (loaded from disk): reattaches to the existing tmux session. If the session is gone (`tmux.ErrSessionNotFound`), the instance is parked as `Paused` instead of erroring — `Storage.LoadInstances` aborts on the first error, so one dead session would hide all the others.
- **`Pause`** (checkout, `c`): commits each worktree's changes, removes the worktrees but keeps the branches, detaches tmux, copies the branch name to the clipboard.
- **`Resume`**: recreates only the worktrees that are missing or invalid (so it doesn't discard uncommitted work), then restores or restarts tmux. Refuses if a branch is checked out in its real repo.
- **`Kill`**: closes tmux, then removes the worktrees, their branches (unless pre-existing), the workspace directory and the Claude settings directory.
  - Claude Code can move a conversation to the background, for example from its agents view, where it outlives tmux. Resuming it elsewhere then fails with "is running in the background".
  - So Kill first reads the pane's conversation id (`paneSessionID`). `stopBackgroundConversation` then polls `claude agents --json` a few times in a goroutine and runs `claude stop <id>` on the matching background entry. It matches by id prefix, or by `--name` plus cwd when the id is unknown.
- A tmux session that disappears, for example because the program exited, makes `HasUpdated`'s capture fail. `SessionEnded` then confirms it, and the metadata tick marks the instance `Paused`, so `r` restarts it. `Preview` treats a vanished session as empty rather than as an error.
- **Fork** (`f`, `NewFork`, Claude only): a new instance continues the source's conversation.
  - `workspace.Fork` copies every worktree of the source with `GitWorktree.Snapshot` / `SetupFrom`. The commits come along, and so do uncommitted and untracked files, left uncommitted. The copy sits on a new branch, keeps the source's base commit, and the source is left untouched. A paused source is copied from its branch tip.
  - It launches with `--resume <session_id> --fork-session`, from the id the status line reported. This works across worktrees of a repo.
  - The fork's appended system prompt names the source's worktrees. The hook redirects edits aimed at another session's workspace (`inOtherWorkspace`) to the fork's own copy.
- **Resume a conversation** (`C`, `ResumeConversation`): starts an instance titled `resume` that runs `claude --resume` without `--name`, which opens Claude's own picker inside the session. Ctrl+W there lists other worktrees' sessions.
  - cs goes straight into typing mode, so the picker takes your keys.
  - Once the status line reports `session_name`, the instance takes that name as its title (`AdoptClaudeName`, made unique and cut to 32 characters). It sends `/rename` back only if the title had to differ.
- `launchArgs` (resume or fork flags) apply to the first `Start(true)` only. `Start` then resets the tmux program, so a later restart begins a new conversation.

### Multi-repo workspaces (`session/workspace/`, fork-specific)

Every workspace has `.claudesquad/workspace.json` (root, branch, `single_repo` for single-repo workspaces) and `repos.json`. The rest of this section is about multi-repo workspaces.

Claude runs in the real directory, so Read/Grep/Glob see every repo as usual. Each repo gets a git worktree only when Claude first changes it.

- **Launch**: `Instance.launchProgram()` appends the session's `--settings` (see Claude status below), which here also carry `Workspace.Hooks()` (a PreToolUse hook running `cs hook --workspace <dir>`), then `Workspace.ClaudeArgs()`: `--add-dir <dir>` and `--append-system-prompt-file`. Hooks passed with `--settings` merge with the user's own; nothing in their settings files is touched.
- **Hook** (`hook.go`, hidden `cs hook` command): it runs in Claude's process, not the TUI's.
  - Edit/Write/MultiEdit/NotebookEdit under `<root>/<repo>/` create the worktree (`Isolate`) and *deny* the call, with a message pointing at the same file in `<dir>/<repo>`. The deny is deliberate: silently rewriting the path would leave Claude reading stale files from the real checkout.
  - Bash creates worktrees only for commands `isRiskyCommand` flags (git writes, `sed -i`, `rm`/`mv`/`cp`, redirects, package installs, `dotnet ef`, formatters). Read-only commands and builds keep running on the real checkout, which has `node_modules`/`bin`.
  - Once a repo is isolated, any Read/Grep/Glob/Bash aimed at its real checkout is redirected.
  - The hook's stdout is Claude Code's response, so `cs hook` must print nothing else there (no `log.Close`).
  - If the workspace no longer exists, the hook denies every tool except Read/Grep/Glob. This happens when a Claude process outlives its killed session. A failing hook would let changes through to the real checkouts.
- **State**: `<dir>/.claudesquad/repos.json` (repo name + base commit) is the source of truth. The hook writes it under a file lock with atomic renames, and the TUI re-reads it on every diff tick. Worktrees are created from each repo's current HEAD on branch `<prefix><title>`.
- **Diff/list**: `DiffStats.Repos` holds one `RepoDiff` per repo with changes; the aggregate `Content` stays empty. The diff tab shows one repo at a time under a selector bar. `←/→` (`KeyPrevRepo`/`KeyNextRepo`) switch repos, and the selection is kept by name across refreshes. In the list, an unselected instance shows `N repos`; the selected one lists each changed repo with its counts, indented below its info row, in whatever height the list has left.
- **Pause/kill**: these loop over the repos via the existing `GitWorktree` methods. `Cleanup` refuses to delete anything outside `~/.claude-squad/workspaces`. `cs reset` calls `workspace.CleanupAll`.
- **Facts verified against Claude Code 2.1.x**:
  - Grep/Glob do not descend into symlinked directories.
  - Edits through a symlink that resolves outside the project need extra permission.
  - `cd` into such a symlink is reset.
  - So workspaces must not symlink repos into a separate folder.
  - Claude's folder-trust dialog defaults to "No, exit", so blindly sending Enter kills the session.

### Claude status (`session/claudestatus/`, fork-specific)

The list's info row shows `<model> · <context used>/<window> · <effort>` for Claude sessions (other agents show their program name). Claude Code only hands these to its status line command, so:

- Every Claude session (`IsClaude`, any mode) is launched with `--settings ~/.claude-squad/sessions/<escaped title>_<hex nanos>/settings.json`. The directory comes from `claudestatus.NewDir` and is stored as `ClaudeDir`; instances saved without it use `<escaped title>`. `Instance.writeClaudeSettings` rewrites the file before every tmux start, and `Kill` removes the directory.
- Its `statusLine` is the hidden `cs statusline --out <dir>/status.json`. It saves `model.display_name`, `effort.level` and `context_window.total_input_tokens` / `context_window_size` (the current context, not a running total), then runs the user's own status line on the same stdin. That command comes from the project's `.claude/settings.local.json`, then `.claude/settings.json`, then `$CLAUDE_CONFIG_DIR` or `~/.claude/settings.json`. Its stdout is what Claude shows, because `--settings` replaces the user's status line rather than merging with it.
- `Info` also keeps `session_id` (used by fork), `session_name` (used by `C`) and `UpdatedAt`.
- Background sessions dispatched from a session (Claude's agents view) inherit its `--settings`, and so its status line, under their own `session_id`.
  - `cs statusline` therefore also saves each report as `<dir>/status/<session_id>.json`.
  - `ComputeClaudeInfo` reads the report of the pane's conversation: tmux `#{pane_pid}` → Claude Code's registry `~/.claude/sessions/<pid>.json` → `sessionId` (`claudestatus.SessionIDForPID`). It falls back to the latest `status.json` when that lookup fails.
  - Process ancestry cannot tell background sessions apart: Claude Code runs interactive sessions in daemon-hosted `--bg-pty-host` processes too.
  - `saveInfo` still drops an empty report, as Claude Code sends while a session starts, if it would replace real numbers for the same conversation.
- `rate_limits` (5-hour and weekly `used_percentage` and `resets_at`) are null until a Claude process makes its first request. `cs statusline --limits <file>` keeps the latest non-null ones in `~/.claude-squad/limits.json`.
- The metadata tick reads `status.json` for every active instance (`ComputeClaudeInfo` → `SetClaudeInfo`).
- The limits belong to the account. The metadata tick reads `limits.json` and the list's title row shows it (`List.SetLimits`). It shows `5h – · wk –` before any limits are known, and 0% for a window whose reset time has passed.

### Persistence (`config/`, `session/storage.go`)

- `~/.claude-squad/config.json` → `config.Config` (default program, `auto_yes`, `daemon_poll_interval`, `branch_prefix`, `profiles`). Created with defaults on first load; `DefaultConfig` resolves the `claude` path via the user's shell.
- `~/.claude-squad/state.json` → `config.State`; instances are stored as raw JSON and decoded by `session.Storage`.
- Only started instances are saved. Adding a persisted field means updating `InstanceData` / `WorkspaceData` **and** both `ToInstanceData` and `FromInstanceData`.

### Event loop and concurrency (`app/app.go`)

- `home.state` is a small state machine (`stateDefault`, `stateNew`, `statePrompt`, `stateHelp`, `stateConfirm`) that drives which overlay (`ui/overlay/`) and menu state are active.
- Expensive I/O stays off the Bubble Tea main loop:
  - `tickUpdateMetadataCmd` self-chains every 500ms. It checks every active instance in parallel goroutines (tmux pane hash for running vs ready, prompt detection, git diff). Only the selected instance gets a full diff; the others get `--numstat` counts to bound memory.
  - Results come back as `metadataUpdateDoneMsg` and are applied in `Update` on the main thread (`SetStatus`, `SetDiffStats`). Never mutate `Instance` state from the background goroutine.
  - `previewTickMsg` refreshes the preview pane every 100ms; new instances start in the background via `runInstanceStartCmd`.
- The right-hand `ui.TabbedWindow` has Preview (captured agent pane, shown at full height with no ellipsis row; if the capture is taller than the pane, the bottom rows), Diff, and Terminal tabs. The Terminal tab keeps a separate shell tmux session per instance in `GetWorkDir()`.
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

`keys/keys.go` is the source of truth (`GlobalKeyStringsMap` + `GlobalkeyBindings`). This fork removed upstream's push key (`p`), along with all `gh` usage; pushing is left to the user or the agent. It added `R` (rename), `f` (fork) and `` ` `` (type into the session).

- **Typing mode** (`stateFocus`): `` ` `` switches to the Preview tab. `handleKeyPress` then sends every key except `` ` `` to the selected session.
  - `keyBytes` turns a key back into terminal bytes. Pastes keep their bracketed-paste markers.
  - `Instance.SendKeys` writes those bytes to the tmux client PTY that cs keeps attached.
  - `View` derives the orange window border (`TabbedWindow.SetFocused`) and the menu (`Menu.SetFocusMode`) from `m.state`. Any state change, such as a help screen popping up, therefore ends typing mode cleanly.
  - Esc reaches the session only after tmux's `escape-time`.
- **Scrolling** (mouse wheel, `shift+↑/↓`, Preview tab): if the pane's program takes mouse events (`#{mouse_any_flag}`, as Claude Code's fullscreen UI does), `ScrollSession` writes SGR wheel events into the tmux client PTY, and tmux passes them to Claude. The transcript is not in tmux's scrollback in that case. Otherwise the preview's own scroll mode reads the scrollback.
  - For `display-message`, target the pane with `-t =<name>:`. `-t=<name>` prints nothing there, although it works for `has-session`.
- `overlay.PlaceOverlay` strips OSC sequences, such as Claude Code's file hyperlinks, from the faded background. Its reflow-based width functions only know CSI sequences, so a hyperlink would count as wide text and push a centered dialog to the right.
  - `fadeLine` then rewrites every SGR sequence in the background to one gray (a dark gray background where the line had one). It does not swap selected color codes, because text after a reset, plain bold text and the terminal's default color would otherwise stay bright behind the dialog.
- cs sets the terminal title to `claude-squad` with OSC 0 at startup and after every detach (`setTerminalTitle`). Bubble Tea's `SetWindowTitle` only sends OSC 2, which macOS Terminal doesn't show on its tab. The bottom menu's `│` groups and highlighted action group come from `Menu.setGroups`, so build options as groups rather than a flat list.

## Contributing

PRs need the CLA signed (CLA Assistant bot). Recent commits use conventional prefixes (`fix:`, `chore:`).
