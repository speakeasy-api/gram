package codemode

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

const (
	// maxQueryBytes bounds tokenization and cursor inputs to 1 KiB.
	maxQueryBytes = 1024
	// maxCursorBytes bounds decoding before allocating a cursor document.
	maxCursorBytes = 512
	// scorerVersion invalidates cursors when ordering rules change.
	scorerVersion = "lexical-v1"
)

type cursor struct {
	Fingerprint string `json:"fingerprint"`
	Offset      int    `json:"offset"`
}

// Rank searches an already authorized live inventory without retaining it.
func Rank(scope string, args SearchArgs, documents []Document, failed []FailedMember) (*SearchPage, error) {
	if len(args.Query) > maxQueryBytes || len(args.Server) > maxQueryBytes || len(documents) > MaxCatalogTools {
		return nil, fmt.Errorf("search input exceeds limit")
	}
	documents = slices.Clone(documents)
	slices.SortFunc(documents, func(a, b Document) int { return strings.Compare(a.Candidate.Path, b.Candidate.Path) })
	failed = slices.Clone(failed)
	slices.SortFunc(failed, func(a, b FailedMember) int { return strings.Compare(a.Server, b.Server) })
	input, err := json.Marshal(struct {
		Scope     string         `json:"scope"`
		Query     string         `json:"query"`
		Server    string         `json:"server"`
		Version   string         `json:"version"`
		Documents []Document     `json:"documents"`
		Failed    []FailedMember `json:"failed"`
	}{Scope: scope, Query: args.Query, Server: args.Server, Version: scorerVersion, Documents: documents, Failed: failed})
	if err != nil || len(input) > MaxCatalogBytes {
		return nil, fmt.Errorf("search inventory exceeds limit")
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(input))
	type ranked struct {
		candidate Candidate
		score     int
	}
	matches := make([]ranked, 0, len(documents))
	query := tokens(args.Query)
	// Distinct terms bound ranking independently of repeated words in a query.
	const maxQueryTerms = 32
	unique := make(map[string]bool)
	terms := make([]string, 0, min(len(query), maxQueryTerms))
	for _, term := range query {
		if unique[term] {
			continue
		}
		unique[term] = true
		terms = append(terms, term)
		if len(terms) > maxQueryTerms {
			return nil, fmt.Errorf("search has too many query terms")
		}
	}
	query = terms
	for _, document := range documents {
		score := relevance(args.Query, query, document)
		if score > 0 || len(query) == 0 {
			matches = append(matches, ranked{candidate: document.Candidate, score: score})
		}
	}
	slices.SortStableFunc(matches, func(a, b ranked) int { return b.score - a.score })
	start, end, next, err := pageBounds(fingerprint, PageArgs{Limit: args.Limit, Cursor: args.Cursor}, len(matches))
	if err != nil {
		return nil, err
	}
	page := &SearchPage{Items: make([]Candidate, 0, end-start), NextCursor: next, Incomplete: len(failed) > 0, FailedMembers: failed}
	for _, match := range matches[start:end] {
		page.Items = append(page.Items, match.candidate)
	}
	if err := boundedJSON(page); err != nil {
		return nil, err
	}
	return page, nil
}

// PaginateServers binds member cursors to the current caller and inventory.
func PaginateServers(scope string, args PageArgs, servers []Server) (*ServerPage, error) {
	servers = slices.Clone(servers)
	slices.SortFunc(servers, func(a, b Server) int { return strings.Compare(a.Slug, b.Slug) })
	raw, err := json.Marshal(servers)
	if err != nil || len(raw) > MaxCatalogBytes {
		return nil, fmt.Errorf("server inventory exceeds limit")
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(append([]byte(scope+"\x00"), raw...)))
	start, end, next, err := pageBounds(fingerprint, args, len(servers))
	if err != nil {
		return nil, err
	}
	page := &ServerPage{Items: append([]Server{}, servers[start:end]...), NextCursor: next}
	if err := boundedJSON(page); err != nil {
		return nil, err
	}
	return page, nil
}

func pageBounds(fingerprint string, args PageArgs, count int) (int, int, string, error) {
	limit := args.Limit
	if limit == 0 {
		limit = DefaultPageSize
	}
	if limit < 1 || limit > MaxPageSize {
		return 0, 0, "", fmt.Errorf("limit must be between 1 and %d", MaxPageSize)
	}
	start := 0
	if args.Cursor != "" {
		if len(args.Cursor) > maxCursorBytes {
			return 0, 0, "", fmt.Errorf("invalid cursor")
		}
		raw, err := base64.RawURLEncoding.DecodeString(args.Cursor)
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid cursor")
		}
		var cur cursor
		if err := json.Unmarshal(raw, &cur); err != nil || cur.Offset < 0 || cur.Offset > count {
			return 0, 0, "", fmt.Errorf("invalid cursor")
		}
		if cur.Fingerprint != fingerprint {
			return 0, 0, "", fmt.Errorf("catalog changed; restart discovery")
		}
		start = cur.Offset
	}
	end := min(start+limit, count)
	next := ""
	if end < count {
		raw, err := json.Marshal(cursor{Fingerprint: fingerprint, Offset: end})
		if err != nil {
			return 0, 0, "", fmt.Errorf("encode cursor: %w", err)
		}
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return start, end, next, nil
}

func boundedJSON(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode callback result: %w", err)
	}
	if len(raw) > MaxResultBytes {
		return fmt.Errorf("callback result exceeds limit")
	}
	return nil
}

func tokens(value string) []string {
	var normalized strings.Builder
	var previous rune
	for _, r := range value {
		if unicode.IsUpper(r) && unicode.IsLower(previous) {
			normalized.WriteByte(' ')
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(unicode.ToLower(r))
		} else {
			normalized.WriteByte(' ')
		}
		previous = r
	}
	return strings.Fields(normalized.String())
}

func relevance(rawQuery string, query []string, document Document) int {
	// Exact paths outrank any token accumulation, which is bounded by the query limit.
	const exactPathScore = 100000
	if strings.EqualFold(strings.TrimSpace(rawQuery), document.Candidate.Path) {
		return exactPathScore
	}
	name := tokenSet(document.Name)
	description := tokenSet(document.Candidate.Description)
	server := tokenSet(document.Candidate.Path + " " + document.ServerName)
	score := 0
	for _, term := range query {
		switch {
		case name[term]:
			score += 12
		case description[term]:
			score += 4
		case server[term]:
			score += 2
		}
	}
	return score
}

func tokenSet(value string) map[string]bool {
	set := make(map[string]bool)
	for _, token := range tokens(value) {
		set[token] = true
	}
	return set
}
