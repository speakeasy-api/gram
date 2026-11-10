package mv

import (
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

// BuildWorkloadCustomFlowsView renders the forms for trusting a platform the
// catalog does not list and allowing its workloads.
func BuildWorkloadCustomFlowsView(flows catalog.CustomFlows) *gen.WorkloadCustomFlows {
	return &gen.WorkloadCustomFlows{
		RegisterPlatform: buildWorkloadFormView(flows.RegisterPlatform),
		EditPlatform:     buildWorkloadFormView(flows.EditPlatform),
		AllowAccess:      buildWorkloadFormView(flows.AllowAccess),
		EditAccess:       buildWorkloadFormView(flows.EditAccess),
	}
}

func buildWorkloadFormView(form catalog.Form) *gen.WorkloadForm {
	steps := make([]*gen.WorkloadFormStep, 0, len(form.Steps))
	for _, step := range form.Steps {
		blocks := make([]*gen.WorkloadFormBlock, 0, len(step.Blocks))
		for _, b := range step.Blocks {
			blocks = append(blocks, &gen.WorkloadFormBlock{
				Type:        string(b.Type),
				Markdown:    b.Markdown,
				Href:        b.Href,
				Label:       b.Label,
				Field:       string(b.Field),
				Placeholder: b.Placeholder,
				Help:        b.Help,
				Multiline:   b.Multiline,
				ReadOnly:    b.ReadOnly,
				Format:      string(b.Format),
			})
		}
		steps = append(steps, &gen.WorkloadFormStep{
			ID:     step.ID,
			Title:  step.Title,
			Blocks: blocks,
		})
	}

	return &gen.WorkloadForm{
		Title:        form.Title,
		Description:  form.Description,
		SubmitLabel:  form.SubmitLabel,
		PendingLabel: form.PendingLabel,
		Steps:        steps,
	}
}
