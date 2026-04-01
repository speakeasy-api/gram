package shared

import (
	. "goa.design/goa/v3/dsl"
)

func CursorPagination() {
	Meta("openapi:extension:x-speakeasy-pagination", `{"type":"cursor","inputs":[{"name":"cursor","in":"parameters","type":"cursor"}],"outputs":{"nextCursor":"$.next_cursor"}}`)
}
