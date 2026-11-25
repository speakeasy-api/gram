package email

// SetupTaskAssignment is sent when someone is assigned an organization setup
// task.
type SetupTaskAssignment struct {
	// AssignerName is the display name of the person who assigned the task.
	AssignerName string
	// OrganizationName is the human-readable name of the organization being set up.
	OrganizationName string
	// TaskTitle is the human-readable title of the assigned setup task.
	TaskTitle string
	// TaskDescription explains the work required for the assigned setup task.
	TaskDescription string
	// SetupLink is the absolute URL to the organization's setup page.
	SetupLink string
}

func (SetupTaskAssignment) Key() TemplateKey {
	return TemplateKeySetupTaskAssignment
}

func (t SetupTaskAssignment) Variables() map[string]string {
	return map[string]string{
		"assigner_name":     t.AssignerName,
		"organization_name": t.OrganizationName,
		"task_title":        t.TaskTitle,
		"task_description":  t.TaskDescription,
		"setup_link":        t.SetupLink,
	}
}

func (SetupTaskAssignment) AddToAudience() bool {
	return false
}
