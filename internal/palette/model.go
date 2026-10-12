package palette

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/DevooKim/herdr-cast/internal/fuzzy"
)

type mode int

const (
	modeList mode = iota
	modePrompt
	modeConfirm
)

type match struct {
	item      *Item
	score     int
	positions []int // rune positions in Title
}

// Loader fetches live Herdr state. It runs after the first frame so the popup
// accepts typing while the server answers.
type Loader func() (*Env, []Item, []string, error)

type loadedMsg struct {
	env      *Env
	items    []Item
	warnings []string
	err      error
}

// Model is the Bubble Tea model for the palette popup.
type Model struct {
	load    Loader
	scopes  []Category // offered by the scope bar and tab, per Config
	loading bool
	env     *Env
	items   []Item
	scope   Category
	input   textinput.Model
	matches []match
	cursor  int
	offset  int
	width   int
	height  int

	mode    mode
	prompt  *Prompt
	confirm *Confirm
	saved   string // list query kept while a prompt is open

	notice  string
	isError bool
	quit    bool
}

const searchPlaceholder = "Search agents, workspaces, tabs, actions, commands, keys…"

func NewModel(load Loader, cfg Config) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = searchPlaceholder
	input.Focus()
	scopes := cfg.scopes()
	scope := loadScope()
	if !slices.Contains(scopes, scope) {
		scope = CatAll
	}
	return &Model{load: load, scopes: scopes, loading: true, scope: scope, input: input, width: 80, height: 20}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, func() tea.Msg {
		env, items, warnings, err := m.load()
		return loadedMsg{env, items, warnings, err}
	})
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		m.loading = false
		if msg.err != nil {
			m.notice, m.isError = msg.err.Error(), true
			return m, nil
		}
		m.env, m.items = msg.env, msg.items
		if len(msg.warnings) > 0 {
			m.notice, m.isError = strings.Join(msg.warnings, "; "), true
		}
		m.refilter()
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(m.width-6, 10)
		m.clampScroll()
		return m, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.mode {
		case modePrompt:
			return m.updatePrompt(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		}
		return m.updateList(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// Keys are matched on msg.Type, never msg.String(): typed or pasted text such
// as "tab" or "end" arrives as KeyRunes whose String() equals a key name.
func (m *Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.refilter()
			return m, nil
		}
		return m, tea.Quit
	case tea.KeyUp, tea.KeyCtrlP, tea.KeyCtrlK:
		m.move(-1)
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN, tea.KeyCtrlJ:
		m.move(1)
		return m, nil
	case tea.KeyPgUp:
		m.move(-m.visibleRows())
		return m, nil
	case tea.KeyPgDown:
		m.move(m.visibleRows())
		return m, nil
	case tea.KeyHome:
		m.cursor = 0
		m.clampScroll()
		return m, nil
	case tea.KeyEnd:
		m.cursor = len(m.matches) - 1
		m.clampScroll()
		return m, nil
	case tea.KeyLeft:
		m.jumpCategory(-1)
		return m, nil
	case tea.KeyRight:
		m.jumpCategory(1)
		return m, nil
	case tea.KeyTab:
		m.cycleScope(1)
		return m, nil
	case tea.KeyShiftTab:
		m.cycleScope(-1)
		return m, nil
	case tea.KeyEnter:
		if m.env == nil && !m.loading {
			return m, tea.Quit // loading failed; nothing to run
		}
		if m.cursor < 0 || m.cursor >= len(m.matches) {
			return m, nil
		}
		return m.apply(m.matches[m.cursor].item.Run(m.env))
	}
	before := m.input.Value()
	cmd := m.updateInput(msg)
	if m.input.Value() != before {
		m.notice = ""
		m.refilter()
	}
	return m, cmd
}

// updateInput feeds a key to the text input. Multi-rune input (an IME commit,
// a paste, or keystrokes read in one chunk) is inserted directly: textinput
// matches keys by String(), so the text "right" would otherwise move the
// cursor instead of being typed.
func (m *Model) updateInput(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
		value := []rune(m.input.Value())
		pos := min(m.input.Position(), len(value))
		next := make([]rune, 0, len(value)+len(msg.Runes))
		next = append(append(append(next, value[:pos]...), msg.Runes...), value[pos:]...)
		m.input.SetValue(string(next))
		m.input.SetCursor(pos + len(msg.Runes))
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *Model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.leaveMode()
		return m, nil
	case tea.KeyEnter:
		value := strings.TrimSpace(m.input.Value())
		if value == "" && !m.prompt.Optional {
			m.notice, m.isError = "a value is required", true
			return m, nil
		}
		prompt := m.prompt
		m.leaveMode()
		return m.apply(prompt.Submit(m.env, value))
	}
	return m, m.updateInput(msg)
}

func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	answer := ""
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
		answer = strings.ToLower(string(msg.Runes))
	}
	switch {
	case msg.Type == tea.KeyEnter || answer == "y":
		confirm := m.confirm
		m.leaveMode()
		return m.apply(confirm.Yes(m.env))
	case msg.Type == tea.KeyEsc || answer == "n":
		m.leaveMode()
	}
	return m, nil
}

// apply turns an item's Outcome into the next palette state.
func (m *Model) apply(out Outcome) (tea.Model, tea.Cmd) {
	switch {
	case out.Err != nil:
		m.notice, m.isError = out.Err.Error(), true
	case out.Prompt != nil:
		m.saved = m.input.Value()
		m.mode, m.prompt = modePrompt, out.Prompt
		m.input.Placeholder = ""
		m.input.SetValue(out.Prompt.Initial)
		m.input.CursorEnd()
		m.notice = ""
	case out.Confirm != nil:
		m.mode, m.confirm = modeConfirm, out.Confirm
		m.notice = ""
	case out.Notice != "":
		m.notice, m.isError = out.Notice, false
	case out.Close:
		m.quit = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) leaveMode() {
	if m.mode == modePrompt {
		m.input.SetValue(m.saved)
		m.input.Placeholder = searchPlaceholder
	}
	m.mode, m.prompt, m.confirm = modeList, nil, nil
	m.notice = ""
}

func (m *Model) cycleScope(delta int) {
	i := slices.Index(m.scopes, m.scope)
	m.scope = m.scopes[wrap(i+delta, len(m.scopes))]
	saveScope(m.scope)
	m.refilter()
}

func (m *Model) move(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.matches)-1)
	m.clampScroll()
}

// jumpCategory moves the cursor to the first row of the next (delta > 0) or
// previous category among the current results, wrapping at the ends.
func (m *Model) jumpCategory(delta int) {
	if len(m.matches) == 0 {
		return
	}
	var present []Category
	first := map[Category]int{}
	for i, mt := range m.matches {
		if _, seen := first[mt.item.Cat]; !seen {
			first[mt.item.Cat] = i
		}
	}
	for _, c := range categories {
		if _, ok := first[c]; ok {
			present = append(present, c)
		}
	}
	current := slices.Index(present, m.matches[m.cursor].item.Cat)
	m.cursor = first[present[wrap(current+delta, len(present))]]
	m.clampScroll()
}

func (m *Model) listHeight() int {
	// input, scope bar, rule above and rule + footer below.
	return max(m.height-5, 1)
}

// Each row takes rowLines lines (title and key, then the detail beneath).
// A one-line divider separates rows of different categories, so the number
// of rows that fit depends on where the list starts.
const rowLines = 2

// divides reports whether a divider sits above row i when the view starts
// at row start.
func (m *Model) divides(start, i int) bool {
	return i > start && m.matches[i].item.Cat != m.matches[i-1].item.Cat
}

// visibleEnd returns the exclusive end of the rows that fit from start.
func (m *Model) visibleEnd(start int) int {
	h, lines := m.listHeight(), 0
	end := start
	for end < len(m.matches) {
		need := rowLines
		if m.divides(start, end) {
			need++
		}
		if lines+need > h && end > start {
			break
		}
		lines += need
		end++
	}
	return end
}

// visibleRows is the page size for pgup/pgdown, assuming no dividers.
func (m *Model) visibleRows() int {
	return max(m.listHeight()/rowLines, 1)
}

func (m *Model) clampScroll() {
	m.offset = min(m.offset, max(len(m.matches)-1, 0))
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	for m.offset < m.cursor && m.cursor >= m.visibleEnd(m.offset) {
		m.offset++
	}
	// Pull the view up when it ends early with room left above.
	for m.offset > 0 && m.visibleEnd(m.offset-1) == len(m.matches) && m.visibleEnd(m.offset) == len(m.matches) {
		m.offset--
	}
}

func (m *Model) refilter() {
	m.matches = filter(m.items, m.scope, m.input.Value())
	m.cursor, m.offset = 0, 0
}

// Filter returns the items the palette would show for scope and query, best
// match first.
func Filter(items []Item, scope Category, query string) []*Item {
	matches := filter(items, scope, query)
	out := make([]*Item, len(matches))
	for i, mt := range matches {
		out[i] = mt.item
	}
	return out
}

func filter(items []Item, scope Category, query string) []match {
	terms := strings.Fields(query)
	var matches []match
	for i := range items {
		item := &items[i]
		if scope != CatAll && item.Cat != scope {
			continue
		}
		if mt, ok := score(item, terms); ok {
			matches = append(matches, mt)
		}
	}
	if len(terms) > 0 {
		slices.SortStableFunc(matches, func(a, b match) int { return b.score - a.score })
	}
	return matches
}

// score requires every whitespace-separated term to hit the item: a fuzzy
// match on the title (ranked and highlighted) or a substring of the secondary
// text (detail, ids, category).
func score(item *Item, terms []string) (match, bool) {
	mt := match{item: item}
	if len(terms) == 0 {
		return mt, true
	}
	secondary := item.Detail + " " + item.Extra + " " + item.Cat.Badge()
	for _, term := range terms {
		if s, pos, ok := fuzzy.Match(term, item.Title); ok {
			mt.score += s + 20
			mt.positions = append(mt.positions, pos...)
			continue
		}
		if !fuzzy.Contains(term, secondary) {
			return mt, false
		}
		mt.score += 8 * len([]rune(term))
	}
	return mt, true
}

// ---- view ----

// cells measures text the way terminals and Herdr draw it. go-runewidth's
// default reads the locale and, under ko_KR/ja_JP/zh_CN, counts ambiguous
// glyphs such as ● · … ─ as two cells, which would misalign every row.
var cells = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

var (
	accent   = lipgloss.AdaptiveColor{Light: "#0b7fa6", Dark: "#7dcfff"}
	dim      = lipgloss.AdaptiveColor{Light: "#7a7a7a", Dark: "#7f849c"}
	selected = lipgloss.AdaptiveColor{Light: "#e4e8ef", Dark: "#2e3346"}
	divider  = lipgloss.AdaptiveColor{Light: "#dce0e8", Dark: "#313244"}
	errColor = lipgloss.AdaptiveColor{Light: "#c4314b", Dark: "#f38ba8"}

	badgeColors = map[Category]lipgloss.AdaptiveColor{
		CatAgent:     {Light: "#8839ef", Dark: "#cba6f7"},
		CatWorkspace: {Light: "#1e66f5", Dark: "#89b4fa"},
		CatTab:       {Light: "#179299", Dark: "#94e2d5"},
		CatAction:    {Light: "#df8e1d", Dark: "#f9e2af"},
		CatCommand:   {Light: "#40a02b", Dark: "#a6e3a1"},
		CatKey:       {Light: "#6c6f85", Dark: "#bac2de"},
	}
	statusDots = map[string]struct {
		glyph string
		color lipgloss.AdaptiveColor
	}{
		"blocked": {"●", lipgloss.AdaptiveColor{Light: "#d20f39", Dark: "#f38ba8"}},
		"working": {"●", lipgloss.AdaptiveColor{Light: "#df8e1d", Dark: "#f9e2af"}},
		"done":    {"●", lipgloss.AdaptiveColor{Light: "#40a02b", Dark: "#a6e3a1"}},
		"idle":    {"○", lipgloss.AdaptiveColor{Light: "#7a7a7a", Dark: "#7f849c"}},
	}
)

func (m *Model) View() string {
	if m.quit {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.viewInput())
	b.WriteByte('\n')
	b.WriteString(m.viewScopes())
	b.WriteByte('\n')
	rule := lipgloss.NewStyle().Foreground(dim).Render(strings.Repeat("─", max(m.width, 1)))
	b.WriteString(rule)
	b.WriteByte('\n')
	b.WriteString(m.viewBody())
	b.WriteString(rule)
	b.WriteByte('\n')
	b.WriteString(m.viewFooter())
	return b.String()
}

func (m *Model) viewInput() string {
	icon := lipgloss.NewStyle().Foreground(accent).Bold(true)
	switch m.mode {
	case modePrompt:
		return icon.Render(" ✎ ") + lipgloss.NewStyle().Bold(true).Render(m.prompt.Label+": ") + m.input.View()
	case modeConfirm:
		return icon.Render(" ? ") + lipgloss.NewStyle().Bold(true).Render(m.confirm.Message)
	}
	return icon.Render(" ❯ ") + m.input.View()
}

func (m *Model) viewScopes() string {
	parts := make([]string, 0, len(m.scopes))
	for _, c := range m.scopes {
		style := lipgloss.NewStyle().Foreground(dim).Padding(0, 1)
		if c == m.scope {
			style = style.Foreground(accent).Bold(true).Underline(true)
		}
		parts = append(parts, style.Render(c.String()))
	}
	return truncateANSI(" "+strings.Join(parts, ""), m.width)
}

func (m *Model) viewBody() string {
	h := m.listHeight()
	var b strings.Builder
	lines := 0
	if m.mode == modeConfirm {
		b.WriteString(lipgloss.NewStyle().Foreground(dim).Render("   enter / y  confirm     esc / n  cancel"))
		b.WriteByte('\n')
		lines++
	} else if len(m.matches) == 0 {
		msg := "   No matches"
		if m.loading {
			msg = "   Loading…"
		} else if m.env == nil {
			msg = "   Could not reach Herdr. Press esc to close."
		}
		b.WriteString(lipgloss.NewStyle().Foreground(dim).Render(msg))
		b.WriteByte('\n')
		lines++
	} else {
		end := m.visibleEnd(m.offset)
		for i := m.offset; i < end; i++ {
			if m.divides(m.offset, i) {
				b.WriteString(m.viewDivider())
				b.WriteByte('\n')
				lines++
			}
			b.WriteString(m.viewRow(m.matches[i], i == m.cursor))
			b.WriteByte('\n')
			lines += rowLines
		}
	}
	for ; lines < h; lines++ {
		b.WriteByte('\n')
	}
	return b.String()
}

// viewDivider is the faint rule between categories, inset to match the rows.
func (m *Model) viewDivider() string {
	rule := strings.Repeat("─", max(m.width-4, 1))
	return "  " + lipgloss.NewStyle().Foreground(divider).Render(rule) + "  "
}

// viewRow renders one row as two lines: status, category, title, and key on
// the first; the detail on the second, aligned under the title.
func (m *Model) viewRow(mt match, isSelected bool) string {
	item := mt.item
	base := lipgloss.NewStyle()
	if isSelected {
		base = base.Background(selected)
	}
	st := func() lipgloss.Style { return base }

	var row strings.Builder
	width := 0
	put := func(s string, style lipgloss.Style) {
		row.WriteString(style.Render(s))
		width += cells.StringWidth(s)
	}
	gutter := func() {
		if isSelected {
			put(" ▌", st().Foreground(accent))
		} else {
			put("  ", st())
		}
	}

	gutter()
	if dot, ok := statusDots[item.Status]; ok {
		put(dot.glyph+" ", st().Foreground(dot.color))
	} else {
		put("  ", st())
	}
	put(fmt.Sprintf("%-6s ", item.Cat.Badge()), st().Foreground(badgeColors[item.Cat]))
	indent := width

	keyLabel := item.Key
	keyWidth := 0
	if keyLabel != "" {
		keyWidth = cells.StringWidth(keyLabel) + 2
	}
	avail := max(m.width-width-keyWidth-1, 4)
	titleStyle := st().Bold(item.Current || isSelected)
	hlStyle := st().Foreground(accent).Bold(true).Underline(true)
	width += writeHighlighted(&row, item.Title, mt.positions, avail, titleStyle, hlStyle)
	if pad := m.width - width - keyWidth; pad > 0 {
		put(strings.Repeat(" ", pad), st())
	}
	if keyLabel != "" {
		put(" "+keyLabel+" ", st().Foreground(accent).Faint(!isSelected))
	}

	row.WriteByte('\n')
	width = 0
	gutter()
	put(strings.Repeat(" ", indent-width), st())
	detail := item.Detail
	if item.Current {
		detail = "current · " + detail
	}
	if rest := m.width - width - 1; rest > 3 && detail != "" {
		put(cells.Truncate(detail, rest, "…"), st().Foreground(dim))
	}
	if pad := m.width - width; pad > 0 {
		put(strings.Repeat(" ", pad), st())
	}
	return row.String()
}

// writeHighlighted renders s within maxWidth cells, styling matched runes,
// and returns the cell width it used.
func writeHighlighted(b *strings.Builder, s string, positions []int, maxWidth int, normal, hl lipgloss.Style) int {
	marked := make(map[int]bool, len(positions))
	for _, p := range positions {
		marked[p] = true
	}
	runes := []rune(s)
	total := cells.StringWidth(s)
	limit := maxWidth
	if total > maxWidth {
		limit = maxWidth - 1 // room for the ellipsis
	}
	used := 0
	var run strings.Builder
	runHL := false
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runHL {
			b.WriteString(hl.Render(run.String()))
		} else {
			b.WriteString(normal.Render(run.String()))
		}
		run.Reset()
	}
	for i, r := range runes {
		w := cells.RuneWidth(r)
		if used+w > limit {
			break
		}
		if marked[i] != runHL {
			flush()
			runHL = marked[i]
		}
		run.WriteRune(r)
		used += w
	}
	flush()
	if total > maxWidth {
		b.WriteString(normal.Render("…"))
		used++
	}
	return used
}

func (m *Model) viewFooter() string {
	style := lipgloss.NewStyle().Foreground(dim)
	var left string
	switch {
	case m.notice != "" && m.isError:
		left = lipgloss.NewStyle().Foreground(errColor).Render(" ✗ " + m.notice)
	case m.notice != "":
		left = lipgloss.NewStyle().Foreground(accent).Render(" ℹ " + m.notice)
	case m.mode == modePrompt:
		left = style.Render(" enter submit · esc back")
	default:
		left = style.Render(" ↑↓ move · ←→ section · enter run · tab scope · esc close")
	}
	right := style.Render(fmt.Sprintf("%d/%d ", len(m.matches), m.scopeTotal()))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncateANSI(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) scopeTotal() int {
	if m.scope == CatAll {
		return len(m.items)
	}
	n := 0
	for i := range m.items {
		if m.items[i].Cat == m.scope {
			n++
		}
	}
	return n
}

func truncateANSI(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
