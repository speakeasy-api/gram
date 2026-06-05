package email

// EnterpriseAdminOnboarding is sent to a prospective enterprise admin to walk
// them through the Gram setup wizard. The single variable is the absolute URL
// of the wizard entry point for their organization.
type EnterpriseAdminOnboarding struct {
	// SetupLink is the absolute URL the recipient clicks to begin the
	// onboarding wizard (e.g. https://app.getgram.ai/<org-slug>/setup).
	SetupLink string
}

func (EnterpriseAdminOnboarding) TransactionalID() TransactionalID {
	return transactionalIDEnterpriseAdminOnboarding
}

func (t EnterpriseAdminOnboarding) Variables() map[string]string {
	return map[string]string{
		"setup_link": t.SetupLink,
	}
}

func (EnterpriseAdminOnboarding) AddToAudience() bool {
	return true
}
