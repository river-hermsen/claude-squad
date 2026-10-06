package main

import (
	"claude-squad/app"
	cmd2 "claude-squad/cmd"
	"claude-squad/config"
	"claude-squad/daemon"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/claudestatus"
	"claude-squad/session/tmux"
	"claude-squad/session/workspace"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	version       = "1.0.20"
	programFlag   string
	autoYesFlag   bool
	daemonFlag    bool
	workspaceFlag string
	statusOutFlag string
	limitsFlag    string
	binName       string
	newOpts       = app.NewSessionOptions{}
	rootCmd       = &cobra.Command{
		Use:   "claude-squad",
		Short: "Claude Squad - Manage multiple AI agents like Claude Code, Aider, Codex, and Amp.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			log.Initialize(daemonFlag)
			defer log.Close()

			if daemonFlag {
				cfg := config.LoadConfig()
				tmux.CaptureMouse = cfg.MouseEnabled()
				err := daemon.RunDaemon(cfg)
				log.ErrorLog.Printf("failed to start daemon %v", err)
				return err
			}

			// Outside a git repository, new sessions run in place instead of in a worktree.
			cfg := config.LoadConfig()
			tmux.CaptureMouse = cfg.MouseEnabled()

			// Program flag overrides config
			program := cfg.GetProgram()
			if programFlag != "" {
				program = programFlag
			}
			// AutoYes flag overrides config
			autoYes := cfg.AutoYes
			if autoYesFlag {
				autoYes = true
			}
			if autoYes {
				defer func() {
					if err := daemon.LaunchDaemon(); err != nil {
						log.ErrorLog.Printf("failed to launch daemon: %v", err)
					}
				}()
			}
			// Kill any daemon that's running.
			if err := daemon.StopDaemon(); err != nil {
				log.ErrorLog.Printf("failed to stop daemon: %v", err)
			}

			return app.Run(ctx, program, autoYes, cfg.MouseEnabled())
		},
	}

	resetCmd = &cobra.Command{
		Use:   "reset",
		Short: "Reset all stored instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			state := config.LoadState()
			storage, err := session.NewStorage(state)
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}
			if err := storage.DeleteAllInstances(); err != nil {
				return fmt.Errorf("failed to reset storage: %w", err)
			}
			fmt.Println("Storage has been reset successfully")

			if err := tmux.CleanupSessions(cmd2.MakeExecutor()); err != nil {
				return fmt.Errorf("failed to cleanup tmux sessions: %w", err)
			}
			fmt.Println("Tmux sessions have been cleaned up")

			if err := workspace.CleanupAll(); err != nil {
				return fmt.Errorf("failed to cleanup workspaces: %w", err)
			}
			fmt.Println("Workspaces have been cleaned up")

			if err := claudestatus.RemoveAll(); err != nil {
				return fmt.Errorf("failed to remove Claude Code session settings: %w", err)
			}

			// Kill any daemon that's running.
			if err := daemon.StopDaemon(); err != nil {
				return err
			}
			fmt.Println("daemon has been stopped")

			return nil
		},
	}

	debugCmd = &cobra.Command{
		Use:   "debug",
		Short: "Print debug information like config paths",
		RunE: func(cmd *cobra.Command, args []string) error {
			log.Initialize(false)
			defer log.Close()

			cfg := config.LoadConfig()

			configDir, err := config.GetConfigDir()
			if err != nil {
				return fmt.Errorf("failed to get config directory: %w", err)
			}
			configJson, _ := json.MarshalIndent(cfg, "", "  ")

			fmt.Printf("Config: %s\n%s\n", filepath.Join(configDir, config.ConfigFileName), configJson)

			return nil
		},
	}

	// hookCmd is the Claude Code PreToolUse hook installed in multi-repo sessions. Claude
	// Code reads its stdout as the hook's response, so it must print nothing else there.
	hookCmd = &cobra.Command{
		Use:    "hook",
		Short:  "Claude Code hook that isolates repositories in a multi-repo session",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			// No log.Close: it prints the log path to stdout.
			log.Initialize(false)
			if err := workspace.RunHook(workspaceFlag, os.Stdin, os.Stdout); err != nil {
				log.ErrorLog.Printf("workspace hook failed: %v", err)
				fmt.Fprintf(os.Stderr, "claude-squad hook: %v\n", err)
				os.Exit(1)
			}
		},
	}

	// statusLineCmd is the Claude Code status line of every Claude session. Claude Code shows
	// its stdout, which is the user's own status line; see claudestatus.RunStatusLine.
	statusLineCmd = &cobra.Command{
		Use:    "statusline",
		Short:  "Claude Code status line that reports a session's model, effort and context use",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			// No log.Close: it prints the log path to stdout. A failure is only logged, so
			// the user's status line still shows.
			log.Initialize(false)
			limitsPath := limitsFlag
			if limitsPath == "" {
				// Settings written before --limits existed.
				limitsPath, _ = claudestatus.LimitsPath()
			}
			if err := claudestatus.RunStatusLine(statusOutFlag, limitsPath, os.Stdin, os.Stdout); err != nil {
				log.ErrorLog.Printf("status line failed: %v", err)
			}
		},
	}

	// newCmd starts a session without the TUI, as a Claude conversation started from the
	// Claude app through the Remote Control server does; see app.NewSession.
	newCmd = &cobra.Command{
		Use:   "new",
		Short: "Start a session without the TUI and print its Remote Control link",
		Long: "Start a session in the current directory, the way the TUI's n does, give it a task once " +
			"it is ready, and print its Remote Control link. The cs TUI shows it in its list.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// No log.Close: it prints the log path, which would end up in what Claude relays.
			log.Initialize(false)
			cmd.SilenceUsage = true
			tmux.CaptureMouse = config.LoadConfig().MouseEnabled()
			return app.NewSession(newOpts, os.Stdout)
		},
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s version %s\n", binName, version)
			fmt.Printf("https://github.com/smtg-ai/claude-squad/releases/tag/v%s\n", version)
		},
	}
)

func init() {
	rootCmd.Flags().StringVarP(&programFlag, "program", "p", "",
		"Program to run in new instances (e.g. 'aider --model ollama_chat/gemma3:1b')")
	rootCmd.Flags().BoolVarP(&autoYesFlag, "autoyes", "y", false,
		"[experimental] If enabled, all instances will automatically accept prompts")
	rootCmd.Flags().BoolVar(&daemonFlag, "daemon", false, "Run a program that loads all sessions"+
		" and runs autoyes mode on them.")

	// Hide the daemonFlag as it's only for internal use
	err := rootCmd.Flags().MarkHidden("daemon")
	if err != nil {
		panic(err)
	}

	hookCmd.Flags().StringVar(&workspaceFlag, "workspace", "", "Workspace directory of the session")
	if err := hookCmd.MarkFlagRequired("workspace"); err != nil {
		panic(err)
	}

	statusLineCmd.Flags().StringVar(&statusOutFlag, "out", "", "File to save the session's status to")
	statusLineCmd.Flags().StringVar(&limitsFlag, "limits", "", "File to save the account's usage limits to")
	if err := statusLineCmd.MarkFlagRequired("out"); err != nil {
		panic(err)
	}

	newCmd.Flags().StringVar(&newOpts.Title, "title", "", "Title of the session (default: from the prompt)")
	newCmd.Flags().StringVar(&newOpts.Prompt, "prompt", "", "Task to give the session once it is ready")
	newCmd.Flags().StringVar(&newOpts.Path, "path", "", "Directory to start the session in (default: the current one)")
	newCmd.Flags().StringVarP(&newOpts.Program, "program", "p", "", "Program to run (default: from the config)")
	newCmd.Flags().BoolVar(&newOpts.RemoteControl, "remote-control", true, "Start a Claude Code session with Remote Control on")

	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(hookCmd)
	rootCmd.AddCommand(statusLineCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(resetCmd)
}

func main() {
	// Extract the binary name from how this was invoked
	binName = filepath.Base(os.Args[0])
	rootCmd.Use = binName

	rootCmd.SilenceErrors = true
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
