package metering

// AttributeChatID identifies the chat containing the metered message.
const AttributeChatID = "chat_id"

// AttributeModel identifies the model that produced or received the message.
const AttributeModel = "model"

// AttributeHookSource identifies the canonical agent source.
const AttributeHookSource = "hook_source"

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

// AttributeMessageUserDirectoryMatch records how the message user's directory profile matched.
const AttributeMessageUserDirectoryMatch = "message_user_directory_match"

// AttributeMessageUserRBACRoles contains the message user's sorted role slugs as JSON.
const AttributeMessageUserRBACRoles = "message_user_rbac_roles"

// AttributeChatOwnerDivisionName identifies the chat owner's active directory division.
const AttributeChatOwnerDivisionName = "chat_owner_division_name"

// AttributeChatOwnerDepartmentName identifies the chat owner's active directory department.
const AttributeChatOwnerDepartmentName = "chat_owner_department_name"

// AttributeChatOwnerDirectoryMatch records how the chat owner's directory profile matched.
const AttributeChatOwnerDirectoryMatch = "chat_owner_directory_match"

// AttributeChatOwnerRBACRoles contains the chat owner's sorted role slugs as JSON.
const AttributeChatOwnerRBACRoles = "chat_owner_rbac_roles"
