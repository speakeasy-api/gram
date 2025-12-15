package security

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/auth"
	. "goa.design/goa/v3/dsl"
)

var (
	FunctionToken = JWTSecurity(auth.FunctionTokenSecurityScheme, func() {
		Description("Gram Functions token based auth.")
	})

	FunctionTokenPayload = func() {
		Token("function_token", String)
	}

	FunctionTokenHeader = func() {
		Header(fmt.Sprintf("function_token:%s", auth.FunctionTokenHeader), String, "Functions token header")
	}

	FunctionTokenNamedHeader = func(name string) {
		Header(fmt.Sprintf("function_token:%s", name), String, "Functions Token header")
	}
)
