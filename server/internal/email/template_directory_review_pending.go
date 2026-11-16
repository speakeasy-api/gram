package email

// DirectoryReviewPending is sent to organization administrators when
// directory changes are waiting for a decision: first when new changes appear,
// then as a reminder while any remain. It carries counts only, never the
// names or attribute values of directory sources.
type DirectoryReviewPending struct {
	// OrganizationName is the human-readable name of the organization.
	OrganizationName string
	// Summary is the headline, such as "3 directory changes need a decision".
	Summary string
	// NewGroupCount is the number of pending new groups.
	NewGroupCount string
	// NewAttributeValueCount is the number of pending new attribute values.
	NewAttributeValueCount string
	// SourceMissingCount is the number of pending mapped sources that have gone
	// from the directory.
	SourceMissingCount string
	// ReviewURL is the absolute URL of the organization's SSO tab, where the
	// changes are listed.
	ReviewURL string
	// IsReminder is "true" for a reminder about changes already announced,
	// otherwise "false".
	IsReminder string
}

func (DirectoryReviewPending) Key() TemplateKey {
	return TemplateKeyDirectoryReviewPending
}

func (t DirectoryReviewPending) Variables() map[string]string {
	return map[string]string{
		"organization_name":         t.OrganizationName,
		"summary":                   t.Summary,
		"new_group_count":           t.NewGroupCount,
		"new_attribute_value_count": t.NewAttributeValueCount,
		"source_missing_count":      t.SourceMissingCount,
		"review_url":                t.ReviewURL,
		"is_reminder":               t.IsReminder,
	}
}

func (DirectoryReviewPending) AddToAudience() bool {
	return false
}
