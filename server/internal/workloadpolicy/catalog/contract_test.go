package catalog

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	srv "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
)

// TestFlowContractsSubmitOnlyWhatTheirFormsAccept holds every editable input
// to an attribute of the request body the flow submits, so a flow cannot
// offer a field its request would drop.
func TestFlowContractsSubmitOnlyWhatTheirFormsAccept(t *testing.T) {
	t.Parallel()

	bodies := map[string]any{
		"register_platform": srv.RegisterIssuerRequestBody{},
		"edit_platform":     srv.UpdateIssuerRequestBody{},
		"allow_access":      srv.AdmitSubjectRequestBody{},
		"edit_access":       srv.UpdateSubjectRequestBody{},
	}
	for _, flow := range flowContracts {
		t.Run(flow.key, func(t *testing.T) {
			t.Parallel()

			body, ok := bodies[flow.key]
			require.True(t, ok, "no request body for flow %s", flow.key)
			accepted := jsonAttributes(reflect.TypeOf(body))

			for field, rule := range flow.contract.fields {
				if rule != fieldRequired && rule != fieldOptional {
					continue
				}
				attribute := string(field)
				if field == FieldLabel {
					// An allowed workload's label is submitted as its name.
					attribute = "name"
				}
				require.Contains(t, accepted, attribute, "%s lets %s be edited, which its request does not accept", flow.key, field)
			}
		})
	}
}

func jsonAttributes(body reflect.Type) []string {
	names := make([]string, 0, body.NumField())
	for field := range body.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		names = append(names, name)
	}
	return names
}
