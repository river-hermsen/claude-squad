package session

import (
	"claude-squad/config"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// The inbox holds instances that `cs new` started outside the TUI, such as for a Claude
// conversation on a phone, until a TUI adds them to its list and saves them. `cs new` cannot
// add them to state.json itself: a running TUI saves its own list there, which would drop them.

// InboxItem is an instance waiting in the inbox.
type InboxItem struct {
	// Path is the item's file.
	Path string
	Data InstanceData
}

func inboxDir() (string, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "inbox"), nil
}

// AddToInbox puts a started instance in the inbox.
func AddToInbox(inst *Instance) error {
	if !inst.Started() {
		return fmt.Errorf("instance '%s' has not been started", inst.Title)
	}
	dir, err := inboxDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(inst.ToInstanceData())
	if err != nil {
		return err
	}
	// Written under a name ReadInbox skips, then renamed, so it is never read half-written.
	path := filepath.Join(dir, fmt.Sprintf("%x.json", time.Now().UnixNano()))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadInbox returns the instances in the inbox, oldest first. Files it cannot read are left
// out.
func ReadInbox() ([]InboxItem, error) {
	dir, err := inboxDir()
	if err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var items []InboxItem
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var data InstanceData
		if err := json.Unmarshal(raw, &data); err != nil || data.Title == "" {
			continue
		}
		items = append(items, InboxItem{Path: path, Data: data})
	}
	return items, nil
}

// Remove takes the item out of the inbox.
func (item InboxItem) Remove() error {
	if err := os.Remove(item.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// StoredTitles returns the titles of the instances saved in state and waiting in the inbox,
// without starting any of them.
func StoredTitles(state config.InstanceStorage) []string {
	var saved []InstanceData
	_ = json.Unmarshal(state.GetInstances(), &saved)
	var titles []string
	for _, data := range saved {
		titles = append(titles, data.Title)
	}
	items, _ := ReadInbox()
	for _, item := range items {
		titles = append(titles, item.Data.Title)
	}
	return titles
}

// UniqueTitle turns name into a title that is not in taken: at most 32 characters, with a
// number added if needed.
func UniqueTitle(name string, taken func(title string) bool) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = "session"
	}
	title := strings.TrimSpace(runewidth.Truncate(name, 32, ""))
	for n := 2; taken(title); n++ {
		suffix := fmt.Sprintf(" %d", n)
		title = strings.TrimSpace(runewidth.Truncate(name, 32-len(suffix), "")) + suffix
	}
	return title
}
