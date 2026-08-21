package email

type PaygActivated struct {
	OrganizationName      string
	TumPricePerMillionUsd string
	ActionURL             string
}

func (PaygActivated) Key() TemplateKey {
	return TemplateKeyPaygActivated
}

func (PaygActivated) AddToAudience() bool { return false }

func (t PaygActivated) Variables() map[string]string {
	return map[string]string{
		"organization_name":         t.OrganizationName,
		"tum_price_per_million_usd": t.TumPricePerMillionUsd,
		"action_url":                t.ActionURL,
	}
}
