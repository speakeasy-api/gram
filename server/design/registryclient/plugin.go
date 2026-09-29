// Package registryclient preserves encoded discovery path segment boundaries.
package registryclient

import (
	"fmt"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/eval"
	httpcodegen "goa.design/goa/v3/http/codegen"
)

func init() { codegen.RegisterPlugin("registry-client-paths", "gen", nil, generate) }

func generate(_ string, _ []eval.Root, files []*codegen.File) ([]*codegen.File, error) {
	count := 0
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "http/registry_discovery/client/encode_decode.go") {
			continue
		}
		for _, section := range file.SectionTemplates {
			if section.Name != "request-builder" {
				continue
			}
			endpoint, ok := section.Data.(*httpcodegen.EndpointData)
			if !ok {
				return nil, fmt.Errorf("registry client request-builder data changed")
			}
			var raw string
			switch endpoint.Method.Name {
			case "discoverVersions":
				raw = `u.RawPath = fmt.Sprintf("/v0.1/servers/%s/versions", url.PathEscape(serverName))`
			case "discoverVersion":
				raw = `u.RawPath = fmt.Sprintf("/v0.1/servers/%s/versions/%s", url.PathEscape(serverName), url.PathEscape(version))`
			default:
				continue
			}
			const anchor = "\n\treq, err := http.NewRequest("
			if strings.Count(endpoint.RequestInit.ClientCode, anchor) != 1 {
				return nil, fmt.Errorf("registry client request initializer changed")
			}
			endpoint.RequestInit.ClientCode = strings.Replace(endpoint.RequestInit.ClientCode, anchor, "\n\t"+raw+anchor, 1)
			count++
		}
	}
	if count != 2 {
		return nil, fmt.Errorf("expected two registry client path initializers, got %d", count)
	}
	return files, nil
}
