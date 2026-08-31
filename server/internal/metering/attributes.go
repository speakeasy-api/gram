package metering

// AttributeChatID identifies the chat containing the metered message.
const AttributeChatID = "chat_id"

// AttributeModel identifies the model that produced or received the message.
const AttributeModel = "model"

// AttributeProvider identifies the AI provider explicitly reported by ingestion.
const AttributeProvider = "provider"

// AttributeHookSource identifies the canonical agent source.
const AttributeHookSource = "hook_source"

// AttributeHookHostname identifies the device hostname explicitly reported by hooks.
const AttributeHookHostname = "hook_hostname"

// AttributeAccountType identifies the AI account classification reported for the session.
const AttributeAccountType = "account_type"

// AttributeBillingMode identifies the AI account billing mode resolved for the session.
const AttributeBillingMode = "billing_mode"

// AttributeMessageUserID identifies the Gram user attached to the message.
const AttributeMessageUserID = "message_user_id"

// AttributeMessageExternalUserID preserves the message actor's opaque external ID.
const AttributeMessageExternalUserID = "message_external_user_id"

// AttributeMessageUserEmail preserves the email explicitly observed by the producer.
const AttributeMessageUserEmail = "message_user_email"

// AttributeBillingUserID identifies the Gram user to whom the producer allocates usage.
const AttributeBillingUserID = "billing_user_id"

// AttributeBillingUserAccountEmail is the current Gram account email for the billing user.
const AttributeBillingUserAccountEmail = "billing_user_account_email"

// AttributeBillingUserDivisionName identifies the billing user's active directory division.
const AttributeBillingUserDivisionName = "billing_user_division_name"

// AttributeBillingUserDepartmentName identifies the billing user's active directory department.
const AttributeBillingUserDepartmentName = "billing_user_department_name"

// AttributeBillingUserJobTitle identifies the billing user's active directory job title.
const AttributeBillingUserJobTitle = "billing_user_job_title"

// AttributeBillingUserEmployeeType identifies the billing user's active directory employee type.
const AttributeBillingUserEmployeeType = "billing_user_employee_type"

// AttributeBillingUserCostCenterName identifies the billing user's active directory cost center.
const AttributeBillingUserCostCenterName = "billing_user_cost_center_name"

// AttributeBillingUserDirectoryGroups contains the billing user's sorted directory groups as JSON.
const AttributeBillingUserDirectoryGroups = "billing_user_directory_groups"

// AttributeBillingUserDirectoryMatch records how the billing user's directory profile matched.
const AttributeBillingUserDirectoryMatch = "billing_user_directory_match"

// AttributeBillingUserRBACRoles contains the billing user's sorted role slugs as JSON.
const AttributeBillingUserRBACRoles = "billing_user_rbac_roles"
