package herdr

import (
	"encoding/json"
	"os"
)

type Workspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      int    `json:"number"`
	Focused     bool   `json:"focused"`
	AgentStatus string `json:"agent_status"`
	TabCount    int    `json:"tab_count"`
	PaneCount   int    `json:"pane_count"`
	ActiveTabID string `json:"active_tab_id"`
}

type Tab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      int    `json:"number"`
	Focused     bool   `json:"focused"`
	AgentStatus string `json:"agent_status"`
	PaneCount   int    `json:"pane_count"`
}

type Pane struct {
	PaneID        string `json:"pane_id"`
	TabID         string `json:"tab_id"`
	WorkspaceID   string `json:"workspace_id"`
	Label         string `json:"label"`
	Focused       bool   `json:"focused"`
	Cwd           string `json:"cwd"`
	ForegroundCwd string `json:"foreground_cwd"`
	AgentStatus   string `json:"agent_status"`
}

type Agent struct {
	PaneID        string `json:"pane_id"`
	TabID         string `json:"tab_id"`
	WorkspaceID   string `json:"workspace_id"`
	Agent         string `json:"agent"`
	Name          string `json:"name"`
	AgentStatus   string `json:"agent_status"`
	StateSeq      int64  `json:"state_change_seq"`
	Focused       bool   `json:"focused"`
	Cwd           string `json:"cwd"`
	ForegroundCwd string `json:"foreground_cwd"`
	Title         string `json:"terminal_title_stripped"`
}

type Snapshot struct {
	Version            string      `json:"version"`
	FocusedWorkspaceID string      `json:"focused_workspace_id"`
	FocusedTabID       string      `json:"focused_tab_id"`
	FocusedPaneID      string      `json:"focused_pane_id"`
	Workspaces         []Workspace `json:"workspaces"`
	Tabs               []Tab       `json:"tabs"`
	Panes              []Pane      `json:"panes"`
	Agents             []Agent     `json:"agents"`
}

type PluginAction struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Contexts    []string `json:"contexts"`
	Platforms   []string `json:"platforms"`
}

type Plugin struct {
	PluginID    string         `json:"plugin_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Enabled     bool           `json:"enabled"`
	Platforms   []string       `json:"platforms"`
	Actions     []PluginAction `json:"actions"`
}

func (c *Client) Snapshot() (*Snapshot, error) {
	var out struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := c.Call("session.snapshot", nil, &out); err != nil {
		return nil, err
	}
	return &out.Snapshot, nil
}

// Agents lists live agents. Prefer it over Snapshot().Agents, which leaves out
// agents that report their own state through pane.report_agent.
func (c *Client) Agents() ([]Agent, error) {
	var out struct {
		Agents []Agent `json:"agents"`
	}
	if err := c.Call("agent.list", nil, &out); err != nil {
		return nil, err
	}
	return out.Agents, nil
}

func (c *Client) Plugins() ([]Plugin, error) {
	var out struct {
		Plugins []Plugin `json:"plugins"`
	}
	if err := c.Call("plugin.list", nil, &out); err != nil {
		return nil, err
	}
	return out.Plugins, nil
}

// InvocationContext is the subset of HERDR_PLUGIN_CONTEXT_JSON the palette
// needs. A popup has no pane of its own, so this names the tiled pane under it.
type InvocationContext struct {
	WorkspaceID    string `json:"workspace_id"`
	TabID          string `json:"tab_id"`
	FocusedPaneID  string `json:"focused_pane_id"`
	FocusedPaneCwd string `json:"focused_pane_cwd"`
	WorkspaceCwd   string `json:"workspace_cwd"`
}

func ContextFromEnv() InvocationContext {
	var ctx InvocationContext
	if raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &ctx)
	}
	return ctx
}
