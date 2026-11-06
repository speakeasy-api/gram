package authclients_test

import (
	"regexp"
	"strings"
	"testing"

	_ "github.com/speakeasy-api/gram/server/design"
	"github.com/speakeasy-api/gram/server/design/authclients"
	"goa.design/goa/v3/eval"
	"goa.design/goa/v3/expr"
	goa "goa.design/goa/v3/pkg"
)

func TestGitHubAppOriginSchema(t *testing.T) {
	if err := eval.RunDSL(); err != nil {
		t.Fatal(err)
	}
	payload := expr.AsObject(expr.Root.Service("authClients").Method("configureGitHubAppAuthClient").Payload.Type)
	configuration := expr.AsObject(authclients.GitHubAppConfiguration.Attribute().Type)
	for name, object := range map[string]*expr.Object{"request": payload, "response": configuration} {
		for _, field := range []string{"instance_url", "upstream_origin"} {
			t.Run(name+"/"+field, func(t *testing.T) {
				validation := object.Attribute(field).Validation
				if validation == nil || validation.Format != expr.FormatURI || validation.Pattern == "" || validation.MaxLength == nil || *validation.MaxLength != 2048 {
					t.Fatalf("expected URI, HTTPS-origin pattern, and max length 2048; got %+v", validation)
				}
				pattern := regexp.MustCompile(validation.Pattern)
				for _, test := range []struct {
					origin string
					valid  bool
				}{
					{"https://github.com", true},
					{"https://github.com/", true},
					{"HTTPS://GITHUB.COM:443/", true},
					{"https://git.example.com:8443", true},
					{"https://localhost:8443/", true},
					{"https://127.0.0.1:8443", true},
					{"https://[2001:db8::1]", true},
					{"https://[2001:db8::1]:8443/", true},
					{"https://[fe80::1%25eth0]:8443", true},
					{"", false},
					{"http://github.com", false},
					{"github.com", false},
					{"https://", false},
					{"https://:443", false},
					{"https://user:secret@github.com", false},
					{"https://user@github.com", false},
					{"https://github.com/api", false},
					{"https://github.com//", false},
					{"https://github.com/%2f", false},
					{"https://github.com?", false},
					{"https://github.com?x=1", false},
					{"https://github.com/#fragment", false},
					{"https://github.com#", false},
					{"https://github.com:invalid", false},
					{"https://github.com:", false},
					{"https://[::1]:", false},
					{"https://git example.com", false},
					{"https://github.com/%zz", false},
					{"https://" + strings.Repeat("a", 2048), false},
				} {
					err := goa.ValidateFormat(field, test.origin, goa.FormatURI)
					valid := err == nil && pattern.MatchString(test.origin) && len(test.origin) <= *validation.MaxLength
					if valid != test.valid {
						t.Errorf("origin %q: valid = %t, want %t (format error: %v)", test.origin, valid, test.valid, err)
					}
				}
			})
		}
	}
	if got := payload.Attribute("instance_url").DefaultValue; got != "https://github.com" {
		t.Errorf("instance_url default = %v", got)
	}
}
