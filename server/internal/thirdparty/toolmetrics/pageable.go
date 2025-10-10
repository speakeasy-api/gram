package toolmetrics

type Pageable interface {
	Cursor() string
	SortOrder() string
	Limit() int
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

func (p *PaginationRequest) SortOrder() string {
	if p.Sort == "ASC" || p.Sort == "DESC" {
		return p.Sort
	}
	return "DESC"
}

func (p *PaginationRequest) Limit() int {
	return p.PerPage + 1 // +1 for detecting if there are more records
}

func (p *PaginationRequest) SetDefaults() {
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
