package security

import (
	"fmt"

	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

var WorkOSSignature = APIKeySecurity(constants.WorkOSSignatureSecurityScheme, func() {
	Description("WorkOS webhook signature.")
})

var WorkOSSignaturePayload = func() {
	APIKey(constants.WorkOSSignatureSecurityScheme, "workos_signature", String)
}

var WorkOSSignatureHeader = func() {
	Header(fmt.Sprintf("workos_signature:%s", constants.WorkOSSignatureHeader), String, "WorkOS webhook signature header")
}

var WorkOSSignatureNamedHeader = func(name string) {
	Header(fmt.Sprintf("workos_signature:%s", name), String, "WorkOS webhook signature header")
}
