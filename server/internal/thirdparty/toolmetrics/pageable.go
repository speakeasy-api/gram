package toolmetrics

import (
	"strings"
)

type Pageable interface {
	Cursor() string
	SortOrder() string // Changed from SortOrder to Sort
	Limit() int
	SetCursors()
	SetDefaults()
}

type PaginationRequest struct {
	PerPage    int           `json:"per_page" validate:"required,gte=1,lte=100"`
	Direction  PageDirection `json:"direction" validate:"omitempty,oneof=next prev"`
	Sort       string        `json:"sort" validate:"omitempty,oneof=ASC DESC"`
	PrevCursor string        `json:"prev_page_cursor" validate:"omitempty"`
	NextCursor string        `json:"next_page_cursor" validate:"omitempty"`
}

type PageDirection string

const (
	Next PageDirection = "next"
	Prev PageDirection = "prev"
)

func (p *PaginationRequest) Cursor() string {
	if p.Direction == Next {
		return p.NextCursor
	}
	return p.PrevCursor
}

// SortOrder Fixed method name to match interface
func (p *PaginationRequest) SortOrder() string {
	if p.Sort == "ASC" || p.Sort == "DESC" {
		return p.Sort
	}
	return "DESC"
}

func (p *PaginationRequest) Limit() int {
	return p.PerPage + 1 // +1 for detecting if there are more records
}

func (p *PaginationRequest) SetCursors() {
	switch p.Sort {
	case "ASC":
		if isStringEmpty(p.NextCursor) {
			p.NextCursor = "" // Start from the beginning for ASC
		}
	default: // DESC
		if isStringEmpty(p.NextCursor) {
			p.NextCursor = "7ZZZZZZZZZZZZZZZZZZZZZZZZZ" // Largest valid ULID for DESC
		}
	}
}

func isStringEmpty(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

func (r *PaginationRequest) SetDefaults() {
	if r.PerPage <= 0 || r.PerPage > 100 {
		r.PerPage = 20
	}
	if r.Sort == "" {
		r.Sort = "created_at"
	}
	if r.Direction == "" {
		r.Direction = Next
	}
}
