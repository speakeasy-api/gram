package transcriptstore

import (
	"strings"
	"unicode"
)

// likePattern is a Postgres ILIKE pattern compiled for matching in Go, so a
// search over encrypted shadows matches what the plaintext SQL matched: '%'
// matches any run of characters, '_' matches one, and a backslash makes the
// next character literal (the default LIKE escape). Matching folds case like
// ILIKE. A trailing lone backslash, which Postgres rejects, matches itself.
type likePattern struct {
	tokens []likeToken
	// contains is the lowered literal when the pattern is '%literal%' with no
	// other wildcard, the shape every search uses, so it can match with
	// strings.Contains instead of the general matcher.
	contains   string
	isContains bool
}

type likeTokenKind int

const (
	likeLiteral likeTokenKind = iota
	likeOne
	likeAny
)

type likeToken struct {
	kind likeTokenKind
	r    rune
}

func compileLike(pattern string) likePattern {
	runes := []rune(pattern)
	tokens := make([]likeToken, 0, len(runes))
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\' && i+1 < len(runes):
			i++
			tokens = append(tokens, likeToken{kind: likeLiteral, r: unicode.ToLower(runes[i])})
		case r == '%':
			// Consecutive '%' match the same as one.
			if len(tokens) > 0 && tokens[len(tokens)-1].kind == likeAny {
				continue
			}
			tokens = append(tokens, likeToken{kind: likeAny, r: 0})
		case r == '_':
			tokens = append(tokens, likeToken{kind: likeOne, r: 0})
		default:
			tokens = append(tokens, likeToken{kind: likeLiteral, r: unicode.ToLower(r)})
		}
	}
	p := likePattern{tokens: tokens, contains: "", isContains: false}
	if len(tokens) < 2 || tokens[0].kind != likeAny || tokens[len(tokens)-1].kind != likeAny {
		return p
	}
	var literal strings.Builder
	for _, tok := range tokens[1 : len(tokens)-1] {
		if tok.kind != likeLiteral {
			return p
		}
		literal.WriteRune(tok.r)
	}
	p.contains = literal.String()
	p.isContains = true
	return p
}

// match reports whether text matches the whole pattern, ignoring case.
func (p likePattern) match(text string) bool {
	lowered := strings.ToLower(text)
	if p.isContains {
		return strings.Contains(lowered, p.contains)
	}
	s := []rune(lowered)
	// Greedy matching that backtracks to the most recent '%', which is enough
	// for LIKE because '%' is the only variable-length token.
	ti, si := 0, 0
	starToken, starText := -1, 0
	for si < len(s) {
		if ti < len(p.tokens) {
			tok := p.tokens[ti]
			if tok.kind == likeAny {
				starToken, starText = ti, si
				ti++
				continue
			}
			if tok.kind == likeOne || tok.r == s[si] {
				ti++
				si++
				continue
			}
		}
		if starToken < 0 {
			return false
		}
		starText++
		ti, si = starToken+1, starText
	}
	for ti < len(p.tokens) && p.tokens[ti].kind == likeAny {
		ti++
	}
	return ti == len(p.tokens)
}

// matchBody reports whether any field the SQL search matched does: the
// message text, the tool calls JSON and the structured content JSON.
func (p likePattern) matchBody(content string, toolCalls, contentRaw []byte) bool {
	if p.match(content) {
		return true
	}
	if toolCalls != nil && p.match(string(toolCalls)) {
		return true
	}
	return contentRaw != nil && p.match(string(contentRaw))
}
