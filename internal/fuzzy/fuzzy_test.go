package fuzzy

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		query, candidate string
		want             bool
	}{
		{"", "anything", true},
		{"spv", "split vertical", true},
		{"SPLIT", "split vertical", true},
		{"vs", "split vertical", false},
		{"ㅇㄷ", "인덱스 DB 구현", true},
		{"인덱", "인덱스 DB 구현", true},
		{"ㅇdb", "인덱스 DB", true},
		{"ㄱ", "abc", false},
	}
	for _, tc := range cases {
		_, _, ok := Match(tc.query, tc.candidate)
		if ok != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.query, tc.candidate, ok, tc.want)
		}
	}
}

func TestContains(t *testing.T) {
	cases := []struct {
		query, candidate string
		want             bool
	}{
		{"Pane", "workspace · 2 panes", true},
		{"ㅇㄷ", "인덱스", true},
		{"alp", "global", false},
		{"", "x", true},
	}
	for _, tc := range cases {
		if got := Contains(tc.query, tc.candidate); got != tc.want {
			t.Errorf("Contains(%q, %q) = %v, want %v", tc.query, tc.candidate, got, tc.want)
		}
	}
}

func TestMatchPrefersBoundaries(t *testing.T) {
	boundary, _, _ := Match("nt", "new tab")
	inner, _, _ := Match("nt", "content")
	if boundary <= inner {
		t.Errorf("boundary score %d should beat inner score %d", boundary, inner)
	}
}

func TestMatchPositions(t *testing.T) {
	_, pos, ok := Match("ab", "xaxb")
	if !ok || len(pos) != 2 || pos[0] != 1 || pos[1] != 3 {
		t.Errorf("positions = %v, ok = %v", pos, ok)
	}
}
