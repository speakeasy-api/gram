package notifications

import (
	. "goa.design/goa/v3/dsl"

	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
)

var _ = Service("notifications", func() {
	Description("Managing project notifications.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("listNotifications", func() {
		Description("List notifications for the current project")

		Payload(func() {
			Attribute("archived", Boolean, "Filter by archived status. If not provided, returns non-archived notifications.", func() {
				Default(false)
			})
			Attribute("limit", Int32, "Maximum number of notifications to return", func() {
				Default(50)
				Maximum(100)
			})
			Attribute("cursor", String, "Cursor for pagination (notification ID)")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(ListNotificationsResult)

		HTTP(func() {
			GET("/rpc/notifications.list")
			Param("archived")
			Param("limit")
			Param("cursor")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listNotifications")
		Meta("openapi:extension:x-speakeasy-name-override", "list")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListNotifications"}`)
	})

	Method("createNotification", func() {
		Description("Create a notification for the current project")

		Payload(func() {
			Extend(CreateNotificationForm)
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(Notification)

		HTTP(func() {
			POST("/rpc/notifications.create")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createNotification")
		Meta("openapi:extension:x-speakeasy-name-override", "create")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "CreateNotification"}`)
	})

	Method("archiveNotification", func() {
		Description("Archive a notification")

		Payload(func() {
			Attribute("id", String, "The notification ID", func() {
				Format(FormatUUID)
			})
			Required("id")
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(Notification)

		HTTP(func() {
			POST("/rpc/notifications.archive")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "archiveNotification")
		Meta("openapi:extension:x-speakeasy-name-override", "archive")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ArchiveNotification"}`)
	})

	Method("getUnreadCount", func() {
		Description("Get the count of notifications created since a given timestamp")

		Payload(func() {
			Attribute("since", String, "ISO timestamp to count notifications from", func() {
				Format(FormatDateTime)
			})
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(UnreadCountResult)

		HTTP(func() {
			GET("/rpc/notifications.unreadCount")
			Param("since")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getUnreadCount")
		Meta("openapi:extension:x-speakeasy-name-override", "unreadCount")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "NotificationsUnreadCount"}`)
	})
})

var NotificationType = Type("NotificationType", String, func() {
	Description("The type of notification")
	Enum("system", "user_action")
})

var NotificationLevel = Type("NotificationLevel", String, func() {
	Description("The severity level of the notification")
	Enum("info", "success", "warning", "error")
})

var Notification = Type("Notification", func() {
	Description("A notification in the system")

	Attribute("id", String, "The notification ID", func() {
		Format(FormatUUID)
	})
	Attribute("projectId", String, "The project ID", func() {
		Format(FormatUUID)
	})
	Attribute("type", NotificationType, "The notification type")
	Attribute("level", NotificationLevel, "The notification level")
	Attribute("title", String, "The notification title")
	Attribute("message", String, "The notification message")
	Attribute("actorUserId", String, "The user ID of the actor who triggered the notification")
	Attribute("resourceType", String, "The type of resource this notification relates to")
	Attribute("resourceId", String, "The ID of the resource this notification relates to")
	Attribute("archivedAt", String, "When the notification was archived", func() {
		Format(FormatDateTime)
	})
	Attribute("createdAt", String, "When the notification was created", func() {
		Format(FormatDateTime)
	})

	Required("id", "projectId", "type", "level", "title", "createdAt")
})

var CreateNotificationForm = Type("CreateNotificationForm", func() {
	Description("Form for creating a new notification")

	Attribute("type", NotificationType, "The notification type")
	Attribute("level", NotificationLevel, "The notification level")
	Attribute("title", String, "The notification title", func() {
		MaxLength(200)
	})
	Attribute("message", String, "The notification message", func() {
		MaxLength(2000)
	})
	Attribute("resourceType", String, "The type of resource this notification relates to", func() {
		MaxLength(50)
	})
	Attribute("resourceId", String, "The ID of the resource this notification relates to", func() {
		MaxLength(100)
	})

	Required("type", "level", "title")
})

var ListNotificationsResult = Type("ListNotificationsResult", func() {
	Description("Result type for listing notifications")

	Attribute("notifications", ArrayOf(Notification), "The list of notifications")
	Attribute("nextCursor", String, "Cursor for the next page of results")

	Required("notifications")
})

var UnreadCountResult = Type("UnreadCountResult", func() {
	Description("Result type for unread notification count")

	Attribute("count", Int32, "The number of unread notifications")

	Required("count")
})
