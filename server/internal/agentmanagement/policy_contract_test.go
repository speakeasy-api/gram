package agentmanagement

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestPolicyGrantRequestSelectorContract(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../gen/http/openapi3.yaml")
	require.NoError(t, err)

	var spec struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Ref string `yaml:"$ref"`
					} `yaml:"schema"`
				} `yaml:"content"`
			} `yaml:"requestBody"`
		} `yaml:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Ref string `yaml:"$ref"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(data, &spec))

	forms := map[string]string{
		"createPolicyGrant": "CreateAgentPolicyGrantForm",
		"updatePolicyGrant": "UpdateAgentPolicyGrantForm",
	}
	for operation, form := range forms {
		body := spec.Paths["/rpc/agents."+operation]["post"].RequestBody
		require.Equal(t, "#/components/schemas/"+form, body.Content["application/json"].Schema.Ref, operation)
		require.Equal(t, "#/components/schemas/AgentPolicySelector", spec.Components.Schemas[form].Properties["selector"].Ref, operation)
	}
}
