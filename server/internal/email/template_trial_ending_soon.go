package email

type TrialEndingSoon struct {
	OrganizationName string
	TrialEndDate     string
	ActionURL        string
}

func (TrialEndingSoon) Key() TemplateKey {
	return TemplateKeyTrialEndingSoon
}

func (TrialEndingSoon) AddToAudience() bool { return false }

func (t TrialEndingSoon) Variables() map[string]string {
	return map[string]string{
		"organization_name": t.OrganizationName,
		"trial_end_date":    t.TrialEndDate,
		"action_url":        t.ActionURL,
	}
}
