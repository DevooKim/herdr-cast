package palette

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func testItems() []Item {
	return []Item{
		{Cat: CatAgent, Title: "claude · 0.6.0 indexed DB 구현", Detail: "my-amaranth › 1  ~/Dev/apps/my-amaranth", Status: "blocked"},
		{Cat: CatTab, Title: "인덱스 DB 구현 · 한글 탭 이름이 아주 긴 경우 테스트", Detail: "alpha › tab 1 · 1 pane", Current: true},
		{Cat: CatKey, Title: "New tab", Detail: "tab", Key: "ctrl+b c"},
		{Cat: CatKey, Title: "Split vertical", Detail: "pane", Key: "ctrl+b v"},
		{Cat: CatCommand, Title: "lazygit", Detail: "popup", Key: "ctrl+b alt+g"},
	}
}

func loadedModel(width int) *Model {
	m := NewModel(func() (*Env, []Item, []string, error) { return nil, nil, nil, nil }, Config{})
	m.Update(loadedMsg{env: &Env{}, items: testItems()})
	m.Update(tea.WindowSizeMsg{Width: width, Height: 20})
	return m
}

func TestRowsFillExactWidth(t *testing.T) {
	for _, width := range []int{40, 61, 100} {
		m := loadedModel(width)
		for i, mt := range m.matches {
			for _, sel := range []bool{false, true} {
				lines := strings.Split(m.viewRow(mt, sel), "\n")
				if len(lines) != rowLines {
					t.Fatalf("row %d has %d lines, want %d", i, len(lines), rowLines)
				}
				for j, line := range lines {
					if got := lipgloss.Width(line); got != width {
						t.Errorf("width %d: row %d line %d (selected=%v) is %d cells: %q", width, i, j, sel, got, line)
					}
				}
			}
		}
	}
}

func TestViewHeightIsStable(t *testing.T) {
	m := loadedModel(60)
	if got := strings.Count(m.View(), "\n") + 1; got != m.height {
		t.Errorf("view has %d lines, want %d", got, m.height)
	}
}

func typeText(m *Model, text string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func TestTextNamedLikeKeysIsTyped(t *testing.T) {
	m := loadedModel(60)
	typeText(m, "tab")
	typeText(m, " right")
	if got := m.input.Value(); got != "tab right" {
		t.Errorf("input = %q, want %q", got, "tab right")
	}
	if m.scope != CatAll {
		t.Errorf("typing \"tab\" changed scope to %v", m.scope)
	}
}

func TestMultiTermQueryMatchesAnyOrder(t *testing.T) {
	m := loadedModel(60)
	typeText(m, "tab new")
	if len(m.matches) == 0 || m.matches[0].item.Title != "New tab" {
		t.Fatalf("top match for \"tab new\" = %v", m.matches)
	}
}

func TestScopeCycling(t *testing.T) {
	m := loadedModel(60)
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.scope != CatKey {
		t.Fatalf("shift+tab from All = %v, want Keybindings", m.scope)
	}
	if len(m.matches) != 2 {
		t.Errorf("keybinding scope shows %d rows, want 2", len(m.matches))
	}
}

func TestVersionBefore(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.9.0", "0.9.2", true},
		{"0.9.2", "0.9.2", false},
		{"0.10.0", "0.9.2", false},
		{"v0.9.1-preview.3", "0.9.2", true},
		{"", "0.9.2", false},
	}
	for _, tc := range cases {
		if got := versionBefore(tc.a, tc.b); got != tc.want {
			t.Errorf("versionBefore(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestScopeIsRemembered(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	m := loadedModel(60)
	if m.scope != CatAll {
		t.Fatalf("initial scope = %v, want All", m.scope)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if next := loadedModel(60); next.scope != CatWorkspace {
		t.Errorf("reopened scope = %v, want Workspaces", next.scope)
	}
}

func TestArrowsJumpBetweenCategories(t *testing.T) {
	m := loadedModel(60) // agent, tab, key, key, cmd
	steps := []struct {
		key  tea.KeyType
		want Category
	}{
		{tea.KeyRight, CatTab},
		{tea.KeyRight, CatCommand},
		{tea.KeyRight, CatKey},
		{tea.KeyRight, CatAgent}, // wraps
		{tea.KeyLeft, CatKey},
		{tea.KeyLeft, CatCommand},
	}
	for i, step := range steps {
		m.Update(tea.KeyMsg{Type: step.key})
		if got := m.matches[m.cursor].item.Cat; got != step.want {
			t.Fatalf("step %d: cursor on %v, want %v", i, got, step.want)
		}
	}
	if m.matches[m.cursor].item.Title != "lazygit" {
		t.Errorf("jump should land on the first row of the category")
	}
}

func TestDisabledScopes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`disabled_scopes = ["keybindings", "tabs", "nope"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warnings := LoadConfig(dir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "nope") {
		t.Errorf("warnings = %v", warnings)
	}
	if !cfg.Hidden(CatKey) || !cfg.Hidden(CatTab) || cfg.Hidden(CatAgent) || cfg.Hidden(CatAll) {
		t.Errorf("Hidden wrong for %v", cfg.DisabledScopes)
	}
	m := NewModel(func() (*Env, []Item, []string, error) { return nil, nil, nil, nil }, cfg)
	m.Update(loadedMsg{env: &Env{}, items: testItems()})
	for range 5 {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
		if m.scope == CatKey || m.scope == CatTab {
			t.Fatalf("tab cycled into disabled scope %v", m.scope)
		}
	}
	if strings.Contains(m.viewScopes(), "Keybindings") {
		t.Error("scope bar still shows Keybindings")
	}
}

func TestMissingConfigIsDefault(t *testing.T) {
	cfg, warnings := LoadConfig(t.TempDir())
	if len(cfg.DisabledScopes) != 0 || len(warnings) != 0 {
		t.Errorf("cfg = %+v, warnings = %v", cfg, warnings)
	}
}

func TestPopupSizeConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("width = \"95%\"\nheight = 40\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warnings := LoadConfig(dir)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	params := cfg.PopupParams()
	if params["width"] != "95%" || params["height"] != 40 {
		t.Errorf("params = %v", params)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("width = \"150%\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, warnings = LoadConfig(dir)
	if len(warnings) != 1 || len(cfg.PopupParams()) != 0 {
		t.Errorf("invalid width: warnings = %v, params = %v", warnings, cfg.PopupParams())
	}
}

func TestDividersOnlyBetweenCategories(t *testing.T) {
	m := loadedModel(60) // agent, tab, key, key, cmd -> grouped: agent | tab | cmd | key key
	m.height = 40
	body := m.viewBody()
	got := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "─") {
			got++
		}
	}
	if got != 3 {
		t.Errorf("dividers = %d, want 3 (agent|tab|key|cmd boundaries)\n%s", got, body)
	}
}

func TestCursorStaysVisibleWithDividers(t *testing.T) {
	m := NewModel(func() (*Env, []Item, []string, error) { return nil, nil, nil, nil }, Config{})
	var items []Item
	for i := range 30 {
		items = append(items, Item{Cat: categories[1+i%6], Title: fmt.Sprintf("item %d", i)})
	}
	m.Update(loadedMsg{env: &Env{}, items: items})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	for i := range len(items) - 1 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
		if m.cursor < m.offset || m.cursor >= m.visibleEnd(m.offset) {
			t.Fatalf("step %d: cursor %d outside view [%d, %d)", i, m.cursor, m.offset, m.visibleEnd(m.offset))
		}
		if got := strings.Count(m.View(), "\n") + 1; got != m.height {
			t.Fatalf("step %d: view has %d lines, want %d", i, got, m.height)
		}
	}
}
