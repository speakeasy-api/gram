package metering

const (
	// AttributeChatID identifies the chat containing the metered message.
	AttributeChatID = "chat_id"

	// AttributeAssistantID identifies the assistant responsible for the workload.
	AttributeAssistantID = "assistant_id"

	// AttributeWorkloadSource identifies the product path responsible for the workload.
	AttributeWorkloadSource = "workload_source"

	// AttributeModel identifies the model that produced or received the message.
	AttributeModel = "model"

	// AttributeProvider identifies the AI provider explicitly reported by ingestion.
	AttributeProvider = "provider"

	// AttributeHookSource identifies the canonical agent source.
	AttributeHookSource = "hook_source"

	// AttributeHookHostname identifies the device hostname explicitly reported by hooks.
	AttributeHookHostname = "hook_hostname"

	// AttributeAccountType identifies the AI account classification reported for the session.
	AttributeAccountType = "account_type"

	// AttributeBillingMode identifies the AI account billing mode resolved for the session.
	AttributeBillingMode = "billing_mode"

	// AttributeMessageUserID identifies the Gram user attached to the message.
	AttributeMessageUserID = "message_user_id"

	// AttributeMessageExternalUserID preserves the message actor's opaque external ID.
	AttributeMessageExternalUserID = "message_external_user_id"

	// AttributeMessageUserEmail preserves the email explicitly observed by the producer.
	AttributeMessageUserEmail = "message_user_email"

	// AttributeBillingUserID identifies the Gram user to whom the producer allocates usage.
	AttributeBillingUserID = "billing_user_id"

	// AttributeBillingUserAccountEmail is the current Gram account email for the billing user.
	AttributeBillingUserAccountEmail = "billing_user_account_email"

	// AttributeBillingUserDivisionName identifies the billing user's active directory division.
	AttributeBillingUserDivisionName = "billing_user_division_name"

	// AttributeBillingUserDepartmentName identifies the billing user's active directory department.
	AttributeBillingUserDepartmentName = "billing_user_department_name"

	// AttributeBillingUserJobTitle identifies the billing user's active directory job title.
	AttributeBillingUserJobTitle = "billing_user_job_title"

	// AttributeBillingUserEmployeeType identifies the billing user's active directory employee type.
	AttributeBillingUserEmployeeType = "billing_user_employee_type"

	// AttributeBillingUserCostCenterName identifies the billing user's active directory cost center.
	AttributeBillingUserCostCenterName = "billing_user_cost_center_name"

	// AttributeBillingUserDirectoryGroups contains the billing user's sorted directory groups as JSON.
	AttributeBillingUserDirectoryGroups = "billing_user_directory_groups"

	// AttributeBillingUserDirectoryMatch records how the billing user's directory profile matched.
	AttributeBillingUserDirectoryMatch = "billing_user_directory_match"

	// AttributeBillingUserRBACRoles contains the billing user's sorted role slugs as JSON.
	AttributeBillingUserRBACRoles = "billing_user_rbac_roles"
)

// WorkloadSource is a stable product-path classification for metered workload.
type WorkloadSource string

const (
	// WorkloadSourceAssistant identifies workload produced by a Gram assistant.
	WorkloadSourceAssistant WorkloadSource = "assistant"

	// WorkloadSourceHook identifies workload captured by a live agent hook.
	WorkloadSourceHook WorkloadSource = "hook"

	// WorkloadSourceImport identifies workload imported from an external provider.
	WorkloadSourceImport WorkloadSource = "import"

	// WorkloadSourceNative identifies workload written through Gram's native chat surface.
	WorkloadSourceNative WorkloadSource = "native"
)
