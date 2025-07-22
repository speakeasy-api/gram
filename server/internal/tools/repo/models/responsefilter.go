package models

import (
	"maps"
	"slices"
)

type FilterType string

const (
	FilterTypeNone FilterType = "none"
	FilterTypeJQ   FilterType = "jq"
)

var FilterTypeValues = slices.Sorted(maps.Values(map[FilterType]string{
	FilterTypeNone: string(FilterTypeNone),
	FilterTypeJQ:   string(FilterTypeJQ),
}))

type ResponseFilter struct {
	Type         FilterType
	Schema       []byte
	StatusCodes  []string
	ContentTypes []string
}

// responseFilterJSON represents the JSON structure stored in the database
type responseFilterJSON struct {
	Type         string   `json:"Type"`
	Schema       string   `json:"Schema"` // base64 encoded
	StatusCodes  []string `json:"StatusCodes"`
	ContentTypes []string `json:"ContentTypes"`
}
