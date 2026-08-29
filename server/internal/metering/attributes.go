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

// AttributeMessageUserAccountEmail is the current Gram account email for the message user.
const AttributeMessageUserAccountEmail = "message_user_account_email"

// AttributeChatOwnerUserID identifies the Gram user that owns the chat.
const AttributeChatOwnerUserID = "chat_owner_user_id"

// AttributeChatOwnerExternalUserID preserves the chat owner's opaque external ID.
const AttributeChatOwnerExternalUserID = "chat_owner_external_user_id"

// AttributeChatOwnerUserEmail is the current Gram account email for the chat owner.
const AttributeChatOwnerUserEmail = "chat_owner_user_email"

// AttributeMessageUserDivisionName identifies the message user's active directory division.
const AttributeMessageUserDivisionName = "message_user_division_name"

// AttributeMessageUserDepartmentName identifies the message user's active directory department.
const AttributeMessageUserDepartmentName = "message_user_department_name"

// AttributeMessageUserJobTitle identifies the message user's active directory job title.
const AttributeMessageUserJobTitle = "message_user_job_title"

// AttributeMessageUserEmployeeType identifies the message user's active directory employee type.
const AttributeMessageUserEmployeeType = "message_user_employee_type"

// AttributeMessageUserCostCenterName identifies the message user's active directory cost center.
const AttributeMessageUserCostCenterName = "message_user_cost_center_name"

// AttributeMessageUserDirectoryGroups contains the message user's sorted directory groups as JSON.
const AttributeMessageUserDirectoryGroups = "message_user_directory_groups"

// AttributeMessageUserDirectoryMatch records how the message user's directory profile matched.
const AttributeMessageUserDirectoryMatch = "message_user_directory_match"

// AttributeMessageUserRBACRoles contains the message user's sorted role slugs as JSON.
const AttributeMessageUserRBACRoles = "message_user_rbac_roles"

// AttributeChatOwnerDivisionName identifies the chat owner's active directory division.
const AttributeChatOwnerDivisionName = "chat_owner_division_name"

// AttributeChatOwnerDepartmentName identifies the chat owner's active directory department.
const AttributeChatOwnerDepartmentName = "chat_owner_department_name"

// AttributeChatOwnerJobTitle identifies the chat owner's active directory job title.
const AttributeChatOwnerJobTitle = "chat_owner_job_title"

// AttributeChatOwnerEmployeeType identifies the chat owner's active directory employee type.
const AttributeChatOwnerEmployeeType = "chat_owner_employee_type"

// AttributeChatOwnerCostCenterName identifies the chat owner's active directory cost center.
const AttributeChatOwnerCostCenterName = "chat_owner_cost_center_name"

// AttributeChatOwnerDirectoryGroups contains the chat owner's sorted directory groups as JSON.
const AttributeChatOwnerDirectoryGroups = "chat_owner_directory_groups"

// AttributeChatOwnerDirectoryMatch records how the chat owner's directory profile matched.
const AttributeChatOwnerDirectoryMatch = "chat_owner_directory_match"

// AttributeChatOwnerRBACRoles contains the chat owner's sorted role slugs as JSON.
const AttributeChatOwnerRBACRoles = "chat_owner_rbac_roles"
