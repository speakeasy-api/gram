package security

import (
	. "goa.design/goa/v3/dsl"
)

const GramKeySecurityScheme = "apikey"

var ByKey = APIKeySecurity(GramKeySecurityScheme, func() {
	Description("key based auth.")
	Scope("consumer", "consumer based tool access")
	Scope("producer", "producer based tool access")
})

var ByKeyPayload = func() {
	APIKey(GramKeySecurityScheme, "apikey_token", String)
}

var ByKeyHeader = func() {
	Header("apikey_token:Gram-Key", String, "API Key header")
}
