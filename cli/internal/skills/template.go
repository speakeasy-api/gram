package skills

import (
	"context"
	"fmt"
	"os"
	"text/template"

	"go.yaml.in/yaml/v4"
)

type SkillsTemplateInfo struct {
	Name         string
	Description  string
	Instructions string
	Examples     []string
}
