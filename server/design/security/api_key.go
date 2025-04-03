package security

import (
	. "goa.design/goa/v3/dsl"
)

const KeySecurityScheme = "apikey"

var ByKey = APIKeySecurity(KeySecurityScheme, func() {
	Description("key based auth.")
	Scope("consumer", "consumer based tool access")
	Scope("producer", "producer based tool access")
})

var ByKeyPayload = func() {
	APIKey(KeySecurityScheme, "apikey_token", String)
}

var ByKeyHeader = func() {
	Header("apikey_token:Gram-Key", String, "API Key header")
}
