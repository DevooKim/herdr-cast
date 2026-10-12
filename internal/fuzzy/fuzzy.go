// Package fuzzy scores a query against a candidate string with an fzf-like
// subsequence match. Matching is case-insensitive, rune-based, and accepts a
// Hangul initial consonant (초성) such as "ㄱ" for any syllable starting with it.
package fuzzy

import (
	"unicode"
)

const (
	scoreMatch       = 16
	bonusBoundary    = 10
	bonusConsecutive = 8
	bonusFirstRune   = 12
	penaltyGapStart  = 3
	penaltyGapExtend = 1
)

// Match reports whether every query rune appears in candidate in order, and
// returns a score (higher is better) plus the matched rune positions.
func Match(query, candidate string) (int, []int, bool) {
	q := []rune(query)
	if len(q) == 0 {
		return 0, nil, true
	}
	c := []rune(candidate)
	if len(q) > len(c) {
		return 0, nil, false
	}

	// Greedy forward scan to find the leftmost end, then a backward scan from
	// there to tighten the match window (the same trick fzf v1 uses).
	qi := 0
	end := -1
	for ci := 0; ci < len(c); ci++ {
		if runeEq(q[qi], c[ci]) {
			qi++
			if qi == len(q) {
				end = ci
				break
			}
		}
	}
	if end < 0 {
		return 0, nil, false
	}
	qi = len(q) - 1
	start := end
	for ci := end; ci >= 0; ci-- {
		if runeEq(q[qi], c[ci]) {
			qi--
			if qi < 0 {
				start = ci
				break
			}
		}
	}

	positions := make([]int, 0, len(q))
	score := 0
	qi = 0
	prev := -2
	inGap := false
	for ci := start; ci <= end && qi < len(q); ci++ {
		if !runeEq(q[qi], c[ci]) {
			if prev >= 0 {
				if inGap {
					score -= penaltyGapExtend
				} else {
					score -= penaltyGapStart
					inGap = true
				}
			}
			continue
		}
		inGap = false
		score += scoreMatch
		if ci == 0 {
			score += bonusFirstRune
		} else if isBoundary(c[ci-1], c[ci]) {
			score += bonusBoundary
		}
		if prev == ci-1 {
			score += bonusConsecutive
		}
		positions = append(positions, ci)
		prev = ci
		qi++
	}
	// Prefer matches that begin early and candidates that are short.
	score -= start / 4
	score -= len(c) / 16
	return score, positions, true
}

// Contains reports whether query occurs contiguously in candidate, with the
// same case folding and 초성 rules as Match. It is a stricter test for
// secondary text, where scattered subsequence hits are mostly noise.
func Contains(query, candidate string) bool {
	q, c := []rune(query), []rune(candidate)
	if len(q) == 0 {
		return true
	}
outer:
	for start := 0; start+len(q) <= len(c); start++ {
		for i, r := range q {
			if !runeEq(r, c[start+i]) {
				continue outer
			}
		}
		return true
	}
	return false
}

func isBoundary(prev, cur rune) bool {
	switch {
	case unicode.IsSpace(prev), prev == '-', prev == '_', prev == '/', prev == '.', prev == ':', prev == '·':
		return true
	case unicode.IsLower(prev) && unicode.IsUpper(cur):
		return true
	case !unicode.IsLetter(prev) && !unicode.IsDigit(prev) && (unicode.IsLetter(cur) || unicode.IsDigit(cur)):
		return true
	}
	return false
}

func runeEq(q, c rune) bool {
	if q == c {
		return true
	}
	if unicode.ToLower(q) == unicode.ToLower(c) {
		return true
	}
	if initial, ok := choseong(c); ok && q == initial {
		return true
	}
	return false
}

// Compatibility jamo for the 19 initial consonants, in syllable-block order.
var choseongTable = []rune("ㄱㄲㄴㄷㄸㄹㅁㅂㅃㅅㅆㅇㅈㅉㅊㅋㅌㅍㅎ")

func choseong(r rune) (rune, bool) {
	if r < 0xAC00 || r > 0xD7A3 {
		return 0, false
	}
	return choseongTable[(r-0xAC00)/(21*28)], true
}
