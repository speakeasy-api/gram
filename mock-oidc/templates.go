package mockoidc

import (
	"embed"
	"fmt"
	"html/template"
)

//go:embed templates/*.html
var templatesFS embed.FS

var challengeTmpl = func() *template.Template {
	t, err := template.ParseFS(templatesFS, "templates/challenge.html")
	if err != nil {
		panic(fmt.Errorf("parse challenge template: %w", err))
	}
	return t
}()

type challengeData struct {
	ClientID   string
	ClientName string
	StateToken string
	Users      []User
}
