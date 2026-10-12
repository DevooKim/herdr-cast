package palette

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/DevooKim/herdr-cast/internal/herdr"
	"github.com/DevooKim/herdr-cast/internal/keymap"
	"github.com/DevooKim/herdr-cast/internal/launch"
)

type Category int

const (
	CatAll Category = iota
	CatAgent
	CatWorkspace
	CatTab
	CatAction
	CatCommand
	CatKey
)

var categories = []Category{CatAll, CatAgent, CatWorkspace, CatTab, CatAction, CatCommand, CatKey}

func (c Category) String() string {
	return [...]string{"All", "Agents", "Workspaces", "Tabs", "Plugin actions", "Commands", "Keybindings"}[c]
}

func (c Category) Badge() string {
	return [...]string{"", "agent", "space", "tab", "plugin", "cmd", "key"}[c]
}

// Item is one palette row.
type Item struct {
	Cat     Category
	Title   string
	Detail  string // dim secondary text
	Key     string // keybinding label shown on the right
	Status  string // agent status, drives the status dot
	Current bool   // the focused workspace, tab, or agent
	Extra   string // searchable text that is not displayed
	Run     func(*Env) Outcome
}

// Outcome is what selecting an item asks the palette to do next.
type Outcome struct {
	Close   bool
	Err     error
	Notice  string
	Prompt  *Prompt
	Confirm *Confirm
}

type Prompt struct {
	Label    string
	Initial  string
	Optional bool
	Submit   func(*Env, string) Outcome
}

type Confirm struct {
	Message string
	Yes     func(*Env) Outcome
}

// Env carries everything an item needs to run.
type Env struct {
	Client   *herdr.Client
	Snap     *herdr.Snapshot
	Ctx      herdr.InvocationContext
	Keymap   *keymap.Keymap
	PluginID string
}

func done(err error) Outcome {
	if err != nil {
		return Outcome{Err: err}
	}
	return Outcome{Close: true}
}

func (e *Env) call(method string, params map[string]any) Outcome {
	return done(e.Client.Call(method, params, nil))
}

// focus moves focus to a workspace, tab, or agent (kind).
func (e *Env) focus(kind, id string) Outcome {
	switch kind {
	case "workspace":
		return e.call("workspace.focus", map[string]any{"workspace_id": id})
	case "tab":
		return e.call("tab.focus", map[string]any{"tab_id": id})
	}
	return e.call("agent.focus", map[string]any{"target": id})
}

func (e *Env) deferJob(job launch.Job) Outcome {
	job.PluginID = e.PluginID
	return done(launch.Defer(job))
}

// workspaceID, tabID, paneID name the tiled location under the popup.
func (e *Env) workspaceID() string {
	return firstNonEmpty(e.Ctx.WorkspaceID, e.Snap.FocusedWorkspaceID)
}
func (e *Env) tabID() string  { return firstNonEmpty(e.Ctx.TabID, e.Snap.FocusedTabID) }
func (e *Env) paneID() string { return firstNonEmpty(e.Ctx.FocusedPaneID, e.Snap.FocusedPaneID) }

func (e *Env) paneCwd() string {
	if e.Ctx.FocusedPaneCwd != "" {
		return e.Ctx.FocusedPaneCwd
	}
	id := e.paneID()
	for _, p := range e.Snap.Panes {
		if p.PaneID == id {
			return firstNonEmpty(p.ForegroundCwd, p.Cwd)
		}
	}
	return ""
}

// commandEnv mirrors the variables Herdr gives [[keys.command]] processes.
func (e *Env) commandEnv() map[string]string {
	env := map[string]string{}
	set := func(k, v string) {
		if v != "" {
			env[k] = v
		}
	}
	set("HERDR_ACTIVE_WORKSPACE_ID", e.workspaceID())
	set("HERDR_ACTIVE_TAB_ID", e.tabID())
	set("HERDR_ACTIVE_PANE_ID", e.paneID())
	set("HERDR_ACTIVE_PANE_CWD", e.paneCwd())
	return env
}

func (e *Env) workspaceLabel(id string) string {
	for _, w := range e.Snap.Workspaces {
		if w.WorkspaceID == id {
			return w.Label
		}
	}
	return id
}

func (e *Env) tabLabel(id string) string {
	for _, t := range e.Snap.Tabs {
		if t.TabID == id {
			return t.Label
		}
	}
	return id
}

// BuildItems assembles every palette row from live Herdr state, skipping
// the categories cfg disables.
func BuildItems(env *Env, plugins []herdr.Plugin, cfg Config) []Item {
	builders := []struct {
		cat   Category
		build func() []Item
	}{
		{CatAgent, func() []Item { return agentItems(env) }},
		{CatWorkspace, func() []Item { return workspaceItems(env) }},
		{CatTab, func() []Item { return tabItems(env) }},
		{CatAction, func() []Item { return pluginActionItems(env, plugins) }},
		{CatCommand, func() []Item { return customCommandItems(env) }},
		{CatKey, func() []Item { return keybindingItems(env) }},
	}
	var items []Item
	for _, b := range builders {
		if !cfg.Hidden(b.cat) {
			items = append(items, b.build()...)
		}
	}
	return items
}

var statusRank = map[string]int{"blocked": 0, "done": 1, "working": 2, "idle": 3, "unknown": 4}

func agentItems(env *Env) []Item {
	agents := slices.Clone(env.Snap.Agents)
	slices.SortStableFunc(agents, func(a, b herdr.Agent) int {
		return rank(a.AgentStatus) - rank(b.AgentStatus)
	})
	items := make([]Item, 0, len(agents))
	for _, a := range agents {
		name := firstNonEmpty(a.Name, a.Agent, "agent")
		title := name
		if a.Title != "" {
			title += " · " + a.Title
		}
		where := env.workspaceLabel(a.WorkspaceID)
		if tab := env.tabLabel(a.TabID); tab != "" && tab != where {
			where += " › " + tab
		}
		target := a.PaneID
		items = append(items, Item{
			Cat:     CatAgent,
			Title:   title,
			Detail:  where + "  " + shortPath(firstNonEmpty(a.ForegroundCwd, a.Cwd)),
			Status:  a.AgentStatus,
			Current: a.PaneID == env.paneID(),
			Extra:   a.Agent + " " + a.AgentStatus + " " + a.PaneID,
			Run:     func(*Env) Outcome { return env.focus("agent", target) },
		})
	}
	return items
}

func rank(status string) int {
	if r, ok := statusRank[status]; ok {
		return r
	}
	return len(statusRank)
}

// Workspaces and tabs keep the server's array order, which is the sidebar and
// tab-bar order; their `number` field is not a position.
func workspaceItems(env *Env) []Item {
	items := make([]Item, 0, len(env.Snap.Workspaces))
	for i, w := range env.Snap.Workspaces {
		id := w.WorkspaceID
		items = append(items, Item{
			Cat:     CatWorkspace,
			Title:   w.Label,
			Detail:  fmt.Sprintf("%d · %s · %s", i+1, plural(w.TabCount, "tab"), plural(w.PaneCount, "pane")),
			Status:  w.AgentStatus,
			Current: id == env.workspaceID(),
			Extra:   id,
			Run:     func(*Env) Outcome { return env.focus("workspace", id) },
		})
	}
	return items
}

func tabItems(env *Env) []Item {
	var items []Item
	for _, w := range env.Snap.Workspaces {
		for i, t := range env.workspaceTabs(w.WorkspaceID) {
			id := t.TabID
			items = append(items, Item{
				Cat:     CatTab,
				Title:   t.Label,
				Detail:  fmt.Sprintf("%s › tab %d · %s", w.Label, i+1, plural(t.PaneCount, "pane")),
				Status:  t.AgentStatus,
				Current: id == env.tabID(),
				Extra:   id,
				Run:     func(*Env) Outcome { return env.focus("tab", id) },
			})
		}
	}
	return items
}

func pluginActionItems(env *Env, plugins []herdr.Plugin) []Item {
	// Show the key a [[keys.command]] plugin_action binding gives an action.
	boundKeys := map[string]string{}
	for _, c := range env.Keymap.Commands {
		if c.Type == "plugin_action" && len(c.Keys) > 0 {
			boundKeys[c.Command] = env.Keymap.Label(c.Keys)
		}
	}
	var items []Item
	for _, p := range plugins {
		if !p.Enabled || p.PluginID == env.PluginID {
			continue
		}
		for _, a := range p.Actions {
			platforms := a.Platforms
			if len(platforms) == 0 {
				platforms = p.Platforms
			}
			if !supportsPlatform(platforms) {
				continue
			}
			qualified := p.PluginID + "." + a.ID
			title := firstNonEmpty(a.Title, a.ID)
			items = append(items, Item{
				Cat:    CatAction,
				Title:  title,
				Detail: firstNonEmpty(p.Name, p.PluginID) + descSuffix(a.Description),
				Key:    boundKeys[qualified],
				Extra:  qualified,
				Run: func(e *Env) Outcome {
					return e.deferJob(launch.Job{Kind: "plugin_action", ActionID: qualified, Title: title})
				},
			})
		}
	}
	return items
}

func customCommandItems(env *Env) []Item {
	var items []Item
	for _, c := range env.Keymap.Commands {
		cmd := c
		// The binding that opens this palette is not worth listing inside it.
		if cmd.Type == "plugin_action" && strings.HasPrefix(cmd.Command, env.PluginID+".") {
			continue
		}
		title := firstNonEmpty(cmd.Description, cmd.Command)
		detail := cmd.Type
		if cmd.Description != "" {
			detail += " · " + cmd.Command
		}
		items = append(items, Item{
			Cat:    CatCommand,
			Title:  title,
			Detail: detail,
			Key:    env.Keymap.Label(cmd.Keys),
			Extra:  cmd.Command,
			Run:    func(e *Env) Outcome { return runCustomCommand(e, cmd) },
		})
	}
	return items
}

func runCustomCommand(e *Env, cmd keymap.CustomCommand) Outcome {
	title := firstNonEmpty(cmd.Description, cmd.Command)
	switch cmd.Type {
	case "shell":
		return done(launch.Shell(cmd.Command, e.paneCwd(), e.commandEnv()))
	case "pane", "popup":
		return e.deferJob(launch.Job{
			Kind:    cmd.Type,
			Command: cmd.Command,
			Cwd:     e.paneCwd(),
			Env:     e.commandEnv(),
			Width:   cmd.Width,
			Height:  cmd.Height,
			Title:   title,
		})
	case "plugin_action":
		return e.deferJob(launch.Job{Kind: "plugin_action", ActionID: cmd.Command, Title: title})
	}
	return Outcome{Err: fmt.Errorf("unsupported command type %q", cmd.Type)}
}

func keybindingItems(env *Env) []Item {
	var items []Item
	known := map[string]bool{}
	for _, spec := range keySpecs {
		known[spec.Name] = true
		if min, ok := minServerVersion[spec.Name]; ok && versionBefore(env.Snap.Version, min) {
			continue
		}
		bindings := env.Keymap.Bindings[spec.Name]
		label := env.Keymap.Label(bindings)
		executable := spec.Run != nil
		// A UI-only action without a key has nothing to offer.
		if !executable && label == "" {
			continue
		}
		detail := spec.Group
		if !executable {
			detail += " · key only"
		}
		if label == "" {
			label = "unbound"
		}
		s := spec
		run := s.Run
		if run == nil {
			run = func(e *Env) Outcome {
				return Outcome{Notice: fmt.Sprintf("“%s” has no API; press %s after closing the palette", s.Title, e.Keymap.Label(e.Keymap.Bindings[s.Name]))}
			}
		}
		items = append(items, Item{
			Cat:    CatKey,
			Title:  s.Title,
			Detail: detail,
			Key:    label,
			Extra:  s.Name,
			Run:    run,
		})
	}
	// Bindings this build does not know yet still deserve a reference row.
	var unknown []string
	for name, bindings := range env.Keymap.Bindings {
		if !known[name] && len(bindings) > 0 {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	for _, name := range unknown {
		label := env.Keymap.Label(env.Keymap.Bindings[name])
		items = append(items, Item{
			Cat:    CatKey,
			Title:  strings.ReplaceAll(name, "_", " "),
			Detail: "other · key only",
			Key:    label,
			Extra:  name,
			Run: func(*Env) Outcome {
				return Outcome{Notice: "press " + label + " after closing the palette"}
			},
		})
	}
	return items
}

func supportsPlatform(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	current := runtime.GOOS
	if current == "darwin" {
		current = "macos"
	}
	return slices.Contains(platforms, current)
}

func descSuffix(desc string) string {
	if desc == "" {
		return ""
	}
	return " · " + desc
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func shortPath(path string) string {
	if path == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "~"
			}
			return "~/" + rel
		}
	}
	return path
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
