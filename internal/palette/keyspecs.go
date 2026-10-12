package palette

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/DevooKim/herdr-cast/internal/herdr"
	"github.com/DevooKim/herdr-cast/internal/launch"
)

// keySpec maps one Herdr keybinding action to the socket call that performs
// it. The mappings follow Herdr's own client (src/client/shell/actions.rs).
// Run is nil for actions that only exist as client UI (settings, copy mode,
// pickers, ...): those rows show the key but cannot be run from a plugin.
type keySpec struct {
	Name  string
	Title string
	Group string
	Run   func(*Env) Outcome
}

// minServerVersion hides actions whose socket method an older running server
// lacks. The server can lag the CLI until it is restarted after an update.
var minServerVersion = map[string]string{
	"clear_pane": "0.9.2", // pane.clear
}

// versionBefore reports whether version a is older than b ("0.9.0" < "0.9.2").
// Unparseable versions are treated as new enough.
func versionBefore(a, b string) bool {
	pa, pb := versionParts(a), versionParts(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func versionParts(v string) []int {
	core, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	fields := strings.Split(core, ".")
	if len(fields) != 3 {
		return nil
	}
	parts := make([]int, 3)
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		parts[i] = n
	}
	return parts
}

var keySpecs = []keySpec{
	{"help", "Show keybindings", "global", nil},
	{"settings", "Open settings", "global", nil},
	{"detach", "Detach", "global", nil},
	{"reload_config", "Reload config", "global", func(e *Env) Outcome {
		return e.call("server.reload_config", nil)
	}},
	{"open_notification_target", "Open notification target", "global", nil},

	{"workspace_picker", "Workspace navigation", "workspace", nil},
	{"goto", "Session navigator", "workspace", nil},
	{"new_workspace", "New workspace", "workspace", newWorkspace},
	{"new_worktree", "New worktree", "workspace", newWorktree},
	{"open_worktree", "Open worktree", "workspace", nil},
	{"remove_worktree", "Delete worktree checkout", "workspace", nil},
	{"rename_workspace", "Rename workspace", "workspace", renameWorkspace},
	{"close_workspace", "Close workspace", "workspace", closeWorkspace},
	{"previous_workspace", "Previous workspace", "workspace", stepWorkspace(-1)},
	{"next_workspace", "Next workspace", "workspace", stepWorkspace(1)},
	{"switch_workspace", "Switch workspace 1-9", "workspace", nil},

	{"previous_agent", "Previous agent", "agent", stepAgent(-1)},
	{"next_agent", "Next agent", "agent", stepAgent(1)},
	{"focus_agent", "Focus agent 1-9", "agent", nil},

	{"new_tab", "New tab", "tab", newTab},
	{"rename_tab", "Rename tab", "tab", renameTab},
	{"previous_tab", "Previous tab", "tab", stepTab(-1)},
	{"next_tab", "Next tab", "tab", stepTab(1)},
	{"move_tab_previous", "Move tab left", "tab", moveTab(-1)},
	{"move_tab_next", "Move tab right", "tab", moveTab(1)},
	{"switch_tab", "Switch tab 1-9", "tab", nil},
	{"close_tab", "Close tab", "tab", closeTab},

	{"split_vertical", "Split vertical", "pane", split("right")},
	{"split_horizontal", "Split horizontal", "pane", split("down")},
	{"close_pane", "Close pane", "pane", closePane},
	{"rename_pane", "Rename pane", "pane", renamePane},
	{"edit_scrollback", "Edit scrollback", "pane", func(e *Env) Outcome {
		// Opens an overlay editor pane, so it runs after the popup closes.
		return e.deferJob(launch.Job{Kind: "api", Method: "pane.edit_scrollback",
			Params: map[string]any{"pane_id": e.paneID()}, Title: "Edit scrollback"})
	}},
	{"clear_pane", "Clear pane", "pane", func(e *Env) Outcome {
		return e.call("pane.clear", map[string]any{"pane_id": e.paneID()})
	}},
	{"copy_mode", "Copy mode", "pane", nil},
	{"zoom", "Zoom pane", "pane", func(e *Env) Outcome {
		return e.call("pane.zoom", map[string]any{"pane_id": e.paneID(), "mode": "toggle"})
	}},
	{"resize_mode", "Resize mode", "pane", nil},
	{"resize_pane_left", "Resize pane left", "pane", paneDirection("pane.resize", "left")},
	{"resize_pane_down", "Resize pane down", "pane", paneDirection("pane.resize", "down")},
	{"resize_pane_up", "Resize pane up", "pane", paneDirection("pane.resize", "up")},
	{"resize_pane_right", "Resize pane right", "pane", paneDirection("pane.resize", "right")},
	{"toggle_sidebar", "Toggle sidebar", "pane", nil},
	{"focus_pane_left", "Focus pane left", "pane", paneDirection("pane.focus_direction", "left")},
	{"focus_pane_down", "Focus pane down", "pane", paneDirection("pane.focus_direction", "down")},
	{"focus_pane_up", "Focus pane up", "pane", paneDirection("pane.focus_direction", "up")},
	{"focus_pane_right", "Focus pane right", "pane", paneDirection("pane.focus_direction", "right")},
	{"swap_pane_left", "Swap pane left", "pane", paneDirection("pane.swap", "left")},
	{"swap_pane_down", "Swap pane down", "pane", paneDirection("pane.swap", "down")},
	{"swap_pane_up", "Swap pane up", "pane", paneDirection("pane.swap", "up")},
	{"swap_pane_right", "Swap pane right", "pane", paneDirection("pane.swap", "right")},
	{"cycle_pane_next", "Cycle pane next", "pane", cyclePane(1)},
	{"cycle_pane_previous", "Cycle pane previous", "pane", cyclePane(-1)},
	{"last_pane", "Last pane", "pane", nil},
}

func newWorkspace(e *Env) Outcome {
	return Outcome{Prompt: &Prompt{
		Label:    "New workspace name (optional)",
		Optional: true,
		Submit: func(e *Env, name string) Outcome {
			params := map[string]any{"focus": true, "source_workspace_id": e.workspaceID()}
			if name != "" {
				params["label"] = name
			}
			return e.call("workspace.create", params)
		},
	}}
}

func newWorktree(e *Env) Outcome {
	return Outcome{Prompt: &Prompt{
		Label: "New worktree branch",
		Submit: func(e *Env, branch string) Outcome {
			return e.call("worktree.create", map[string]any{
				"workspace_id": e.workspaceID(), "branch": branch, "focus": true,
			})
		},
	}}
}

func renameWorkspace(e *Env) Outcome {
	id := e.workspaceID()
	return Outcome{Prompt: &Prompt{
		Label:   "Rename workspace",
		Initial: e.workspaceLabel(id),
		Submit: func(e *Env, label string) Outcome {
			return e.call("workspace.rename", map[string]any{"workspace_id": id, "label": label})
		},
	}}
}

func closeWorkspace(e *Env) Outcome {
	id := e.workspaceID()
	return Outcome{Confirm: &Confirm{
		Message: fmt.Sprintf("Close workspace “%s” and every pane in it?", e.workspaceLabel(id)),
		Yes: func(e *Env) Outcome {
			err := e.Client.Call("workspace.close", map[string]any{"workspace_id": id, "close_group": false}, nil)
			if herdr.IsCode(err, "workspace_group_close_required") {
				return Outcome{Confirm: &Confirm{
					Message: "This workspace has linked worktree workspaces. Close the whole group?",
					Yes: func(e *Env) Outcome {
						return e.call("workspace.close", map[string]any{"workspace_id": id, "close_group": true})
					},
				}}
			}
			return done(err)
		},
	}}
}

func stepWorkspace(delta int) func(*Env) Outcome {
	return func(e *Env) Outcome {
		workspaces := e.Snap.Workspaces
		if len(workspaces) == 0 {
			return Outcome{Err: errors.New("no workspaces")}
		}
		current := slices.IndexFunc(workspaces, func(w herdr.Workspace) bool { return w.WorkspaceID == e.workspaceID() })
		next := wrap(max(current, 0)+delta, len(workspaces))
		return e.call("workspace.focus", map[string]any{"workspace_id": workspaces[next].WorkspaceID})
	}
}

func stepAgent(delta int) func(*Env) Outcome {
	return func(e *Env) Outcome {
		agents := sidebarAgentOrder(e)
		if len(agents) == 0 {
			return Outcome{Err: errors.New("no agents")}
		}
		current := slices.IndexFunc(agents, func(a herdr.Agent) bool { return a.PaneID == e.paneID() })
		var next int
		switch {
		case current >= 0:
			next = wrap(current+delta, len(agents))
		case delta < 0:
			next = len(agents) - 1
		}
		return e.call("agent.focus", map[string]any{"target": agents[next].PaneID})
	}
}

// sidebarAgentOrder mirrors Herdr's agent panel: server order by default, or
// status priority then most recent state change with agent_panel_sort =
// "priority". (An active agent view's custom order is not visible to plugins.)
func sidebarAgentOrder(e *Env) []herdr.Agent {
	agents := slices.Clone(e.Snap.Agents)
	if e.Keymap.AgentPanelSort == "priority" {
		slices.SortStableFunc(agents, func(a, b herdr.Agent) int {
			if d := rank(a.AgentStatus) - rank(b.AgentStatus); d != 0 {
				return d
			}
			return int(b.StateSeq - a.StateSeq)
		})
	}
	return agents
}

// workspaceTabs lists a workspace's tabs in tab-bar order.
func (e *Env) workspaceTabs(workspaceID string) []herdr.Tab {
	var tabs []herdr.Tab
	for _, t := range e.Snap.Tabs {
		if t.WorkspaceID == workspaceID {
			tabs = append(tabs, t)
		}
	}
	return tabs
}

func newTab(e *Env) Outcome {
	return Outcome{Prompt: &Prompt{
		Label:    "New tab name (optional)",
		Optional: true,
		Submit: func(e *Env, name string) Outcome {
			params := map[string]any{"workspace_id": e.workspaceID(), "focus": true}
			if name != "" {
				params["label"] = name
			}
			return e.call("tab.create", params)
		},
	}}
}

func renameTab(e *Env) Outcome {
	id := e.tabID()
	return Outcome{Prompt: &Prompt{
		Label:   "Rename tab",
		Initial: e.tabLabel(id),
		Submit: func(e *Env, label string) Outcome {
			return e.call("tab.rename", map[string]any{"tab_id": id, "label": label})
		},
	}}
}

func stepTab(delta int) func(*Env) Outcome {
	return func(e *Env) Outcome {
		tabs := e.workspaceTabs(e.workspaceID())
		current := slices.IndexFunc(tabs, func(t herdr.Tab) bool { return t.TabID == e.tabID() })
		if current < 0 {
			return Outcome{Err: errors.New("focused tab not found")}
		}
		return e.call("tab.focus", map[string]any{"tab_id": tabs[wrap(current+delta, len(tabs))].TabID})
	}
}

func moveTab(delta int) func(*Env) Outcome {
	return func(e *Env) Outcome {
		tabs := e.workspaceTabs(e.workspaceID())
		source := slices.IndexFunc(tabs, func(t herdr.Tab) bool { return t.TabID == e.tabID() })
		if source < 0 || len(tabs) <= 1 {
			return Outcome{Notice: "nothing to move"}
		}
		// insert_index counts positions before removal, like Herdr's client.
		var insert int
		switch {
		case delta > 0 && source+1 >= len(tabs):
			insert = 0
		case delta > 0:
			insert = source + 2
		case source == 0:
			insert = len(tabs)
		default:
			insert = source - 1
		}
		return e.call("tab.move", map[string]any{"tab_id": e.tabID(), "insert_index": insert})
	}
}

func closeTab(e *Env) Outcome {
	id := e.tabID()
	return Outcome{Confirm: &Confirm{
		Message: fmt.Sprintf("Close tab “%s” and every pane in it?", e.tabLabel(id)),
		Yes: func(e *Env) Outcome {
			return e.call("tab.close", map[string]any{"tab_id": id})
		},
	}}
}

func split(direction string) func(*Env) Outcome {
	return func(e *Env) Outcome {
		return e.call("pane.split", map[string]any{
			"workspace_id":   e.workspaceID(),
			"target_pane_id": e.paneID(),
			"direction":      direction,
			"focus":          true,
		})
	}
}

func closePane(e *Env) Outcome {
	id := e.paneID()
	return Outcome{Confirm: &Confirm{
		Message: fmt.Sprintf("Close pane %s?", id),
		Yes: func(e *Env) Outcome {
			return e.call("pane.close", map[string]any{"pane_id": id})
		},
	}}
}

func renamePane(e *Env) Outcome {
	id := e.paneID()
	initial := ""
	for _, p := range e.Snap.Panes {
		if p.PaneID == id {
			initial = p.Label
		}
	}
	return Outcome{Prompt: &Prompt{
		Label:    "Rename pane (empty clears the name)",
		Initial:  initial,
		Optional: true,
		Submit: func(e *Env, label string) Outcome {
			params := map[string]any{"pane_id": id}
			if label != "" {
				params["label"] = label
			}
			return e.call("pane.rename", params)
		},
	}}
}

func paneDirection(method, direction string) func(*Env) Outcome {
	return func(e *Env) Outcome {
		return e.call(method, map[string]any{"pane_id": e.paneID(), "direction": direction})
	}
}

func cyclePane(delta int) func(*Env) Outcome {
	return func(e *Env) Outcome {
		var panes []herdr.Pane
		for _, p := range e.Snap.Panes {
			if p.TabID == e.tabID() {
				panes = append(panes, p)
			}
		}
		if len(panes) == 0 {
			return Outcome{Err: errors.New("no panes in this tab")}
		}
		current := max(slices.IndexFunc(panes, func(p herdr.Pane) bool { return p.PaneID == e.paneID() }), 0)
		return e.call("pane.focus", map[string]any{"pane_id": panes[wrap(current+delta, len(panes))].PaneID})
	}
}

func wrap(i, n int) int {
	return ((i % n) + n) % n
}
