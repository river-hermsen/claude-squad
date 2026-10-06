package app

import (
	"claude-squad/config"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// NewSessionOptions are the options of `cs new`.
type NewSessionOptions struct {
	// Title is the session's title; it is made from Prompt if empty, and unique either way.
	Title string
	// Prompt is the task the session gets once it is ready, if any.
	Prompt string
	// Path is the directory the session works in; the current one if empty.
	Path string
	// Program is the program to run; the config's if empty.
	Program string
	// RemoteControl starts a Claude Code session with Remote Control on.
	RemoteControl bool
}

const (
	// readyTimeout is how long `cs new` waits for Claude Code to be ready for its task.
	readyTimeout = 90 * time.Second
	// remoteURLTimeout is how long `cs new` waits for Remote Control to connect.
	remoteURLTimeout = 30 * time.Second
)

// NewSession is `cs new`: it starts a session without the TUI, the way the TUI starts one in
// the same directory, as when a Claude conversation on a phone asks for one. It puts the
// session in the inbox, where a running TUI picks it up at once and a later one on start; gives
// it its task once Claude Code is ready; and writes what happened, with the session's Remote
// Control link, to out.
func NewSession(opts NewSessionOptions, out io.Writer) error {
	cfg := config.LoadConfig()
	program := opts.Program
	if program == "" {
		program = cfg.GetProgram()
	}
	path := opts.Path
	if path == "" {
		path = "."
	}

	taken := session.StoredTitles(config.LoadState())
	if len(taken) >= GlobalInstanceLimit {
		return fmt.Errorf("cs already has %d sessions, its limit: kill one first", len(taken))
	}
	name := opts.Title
	if name == "" {
		name, _, _ = strings.Cut(strings.TrimSpace(opts.Prompt), "\n")
	}
	title := session.UniqueTitle(name, func(title string) bool {
		return slices.Contains(taken, title) || tmux.NewTmuxSession(title, "").DoesSessionExist()
	})

	inst, err := session.NewInstance(session.InstanceOptions{
		Title:         title,
		Path:          path,
		Program:       program,
		RemoteControl: opts.RemoteControl && session.IsClaudeProgram(program),
	})
	if err != nil {
		return err
	}
	if err := inst.Start(true); err != nil {
		return fmt.Errorf("could not start session '%s': %w", title, err)
	}
	if err := session.AddToInbox(inst); err != nil {
		// No TUI would ever show it.
		_ = inst.Kill()
		return fmt.Errorf("could not hand session '%s' to cs: %w", title, err)
	}
	fmt.Fprintf(out, "Started session '%s' in %s", title, inst.GetWorkDir())
	if inst.Branch != "" {
		fmt.Fprintf(out, " on branch %s", inst.Branch)
	}
	fmt.Fprintln(out, ".")

	if opts.Prompt != "" {
		err := inst.WaitUntilReady(readyTimeout)
		if errors.Is(err, session.ErrNeedsTrust) {
			return fmt.Errorf("Claude Code in session '%s' asks whether to trust %s; answer it in cs, then give it the task", title, inst.GetWorkDir())
		}
		if err != nil {
			return fmt.Errorf("session '%s' did not get its task: %w", title, err)
		}
		if err := inst.SendTask(opts.Prompt); err != nil {
			return fmt.Errorf("session '%s' did not get its task: %w", title, err)
		}
		fmt.Fprintln(out, "It got the task and is working on it.")
	}

	switch url := inst.WaitForRemoteURL(remoteURLTimeout); {
	case url != "":
		fmt.Fprintf(out, "Continue it at %s\n", url)
	case inst.RemoteControl:
		fmt.Fprintln(out, "Remote Control has not connected yet; in cs, w shows the link once it has.")
	}
	fmt.Fprintln(out, "It shows in the cs list on this machine.")
	return nil
}

// canAdoptInbox reports whether the list can take sessions from the inbox now: not while a
// new instance is being named or started, which takes the list's last place until it starts.
func (m *home) canAdoptInbox() bool {
	if m.state == stateNew || m.state == statePrompt {
		return false
	}
	for _, inst := range m.list.GetInstances() {
		if !inst.Started() {
			return false
		}
	}
	return true
}

// adoptInbox adds the sessions `cs new` started, waiting in the inbox, to the list and saves
// them. It reports whether it added any.
func (m *home) adoptInbox(items []session.InboxItem) bool {
	var adopted []session.InboxItem
	for _, item := range items {
		if slices.ContainsFunc(m.list.GetInstances(), func(inst *session.Instance) bool { return inst.Title == item.Data.Title }) {
			log.WarningLog.Printf("session '%s' from cs new is already in the list", item.Data.Title)
			adopted = append(adopted, item)
			continue
		}
		inst, err := session.FromInstanceData(item.Data)
		if err != nil {
			log.ErrorLog.Printf("could not add session '%s' from cs new: %v", item.Data.Title, err)
			adopted = append(adopted, item)
			continue
		}
		if m.autoYes {
			inst.AutoYes = true
		}
		m.list.AddInstance(inst)
		log.InfoLog.Printf("added session '%s', started with cs new", inst.Title)
		adopted = append(adopted, item)
	}
	if len(adopted) == 0 {
		return false
	}
	// Only once the list is saved can the inbox let go of them.
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		log.ErrorLog.Printf("could not save sessions from cs new: %v", err)
		return true
	}
	for _, item := range adopted {
		if err := item.Remove(); err != nil {
			log.ErrorLog.Printf("could not remove %s from the inbox: %v", item.Path, err)
		}
	}
	return true
}
