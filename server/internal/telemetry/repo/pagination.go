package repo

type Pagination struct {
	PerPage    int           `json:"per_page" validate:"required,gte=1,lte=100"`
	Direction  PageDirection `json:"direction" validate:"omitempty,oneof=next prev"`
	Sort       string        `json:"sort" validate:"omitempty,oneof=ASC DESC"`
	PrevCursor string        `json:"prev_page_cursor" validate:"omitempty"`
	NextCursor string        `json:"next_page_cursor" validate:"omitempty"`
}

type PageDirection string

// PaginationMetadata contains pagination metadata for list results.
type PaginationMetadata struct {
	PerPage        int     `json:"per_page"`
	HasNextPage    bool    `json:"has_next_page"`
	NextPageCursor *string `json:"next_page_cursor,omitempty"`
}

const (
	Next PageDirection = "next"
	Prev PageDirection = "prev"
)

func (p *Pagination) Cursor() string {
	if p.Direction == Next {
		return p.NextCursor
	}
	return p.PrevCursor
}

func (p *Pagination) SortOrder() string {
	if p.Sort == "ASC" || p.Sort == "DESC" {
		return p.Sort
	}
	return "DESC"
}

func (p *Pagination) Limit() int {
	return p.PerPage + 1 // +1 for detecting if there are more records
}

func (p *Pagination) SetDefaults() {
	if p.PerPage <= 0 || p.PerPage > 100 {
		p.PerPage = 20
	}
	if p.Sort == "" {
		p.Sort = "DESC"
	}
	if p.Direction == "" {
		p.Direction = Next
	}
}
