package email

type AccessPaused struct {
	OrganizationName string
	ActionURL        string
}

func (AccessPaused) Key() TemplateKey {
	return TemplateKeyAccessPaused
}

func (AccessPaused) AddToAudience() bool { return false }

func (t AccessPaused) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"action_url":        t.ActionURL,
	}
}
