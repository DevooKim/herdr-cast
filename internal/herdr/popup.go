package herdr

import (
	"fmt"
	"strconv"
	"strings"
)

// PopupSize converts a size written as "80%" or "120" (terminal cells) to the
// shape plugin.pane.open expects: a percentage string or an integer. An empty
// value returns nil, meaning "use the default".
func PopupSize(value string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if pct, ok := strings.CutSuffix(value, "%"); ok {
		if n, err := strconv.Atoi(pct); err == nil && n >= 1 && n <= 100 {
			return strconv.Itoa(n) + "%", nil
		}
		return nil, fmt.Errorf("invalid popup size %q: percentages run from 1%% to 100%%", value)
	}
	if n, err := strconv.Atoi(value); err == nil && n > 0 && n <= 65535 {
		return n, nil
	}
	return nil, fmt.Errorf("invalid popup size %q: use cells like 120 or a percentage like \"80%%\"", value)
}
