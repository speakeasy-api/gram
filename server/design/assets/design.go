package assets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("assets", func() {
	Description("Manages assets used by Gram projects.")
	shared.DeclareErrorResponses()

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	Security(security.Session, security.ProjectSlug)

	Method("serveImage", func() {
		Description("Serve an image from Gram.")

		Payload(ServeImageForm)
		Result(ServeImageResult)

		// We remove security for images because they are considered public
		// The rest of these methods will inherit security from the service
		NoSecurity()

		HTTP(func() {
			GET("/rpc/assets.serveImage")
			Param("id")

			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("content_length:Content-Length")
				Header("last_modified:Last-Modified")
				Header("access_control_allow_origin:Access-Control-Allow-Origin")
			})

			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "serveImage")
		Meta("openapi:extension:x-speakeasy-name-override", "serveImage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "serveImage"}`)
	})

	Method("uploadImage", func() {

		Description("Upload an image to Gram.")

		Payload(UploadImageForm)

		Result(UploadImageResult)

		HTTP(func() {
			POST("/rpc/assets.uploadImage")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			security.ByKeyHeader()
			security.ProjectHeader()
			security.SessionHeader()
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadImage")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadImage")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadImage"}`)
	})

	Method("uploadFunctions", func() {
		Description("Upload functions to Gram.")

		Payload(UploadFunctionsForm)

		Result(UploadFunctionsResult)

		HTTP(func() {
			POST("/rpc/assets.uploadFunctions")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			security.ByKeyHeader()
			security.ProjectHeader()
			security.SessionHeader()
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadFunctions")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadFunctions")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadFunctions"}`)
	})

	Method("uploadOpenAPIv3", func() {
		Description("Upload an OpenAPI v3 document to Gram.")

		Payload(UploadOpenAPIv3Form)

		Result(UploadOpenAPIv3Result)

		HTTP(func() {
			POST("/rpc/assets.uploadOpenAPIv3")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			security.ByKeyHeader()
			security.ProjectHeader()
			security.SessionHeader()
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadOpenAPIv3Asset")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadOpenAPIv3")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadOpenAPIv3"}`)
	})

	Method("fetchOpenAPIv3FromURL", func() {
		Description("Fetch an OpenAPI v3 document from a URL and upload it to Gram.")

		Payload(FetchOpenAPIv3FromURLForm)

		Result(UploadOpenAPIv3Result)

		HTTP(func() {
			POST("/rpc/assets.fetchOpenAPIv3FromURL")
			security.ByKeyHeader()
			security.ProjectHeader()
			security.SessionHeader()
		})

		Meta("openapi:operationId", "fetchOpenAPIv3FromURL")
		Meta("openapi:extension:x-speakeasy-name-override", "fetchOpenAPIv3FromURL")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "FetchOpenAPIv3FromURL"}`)
	})

	Method("serveOpenAPIv3", func() {
		Description("Serve an OpenAPIv3 asset from Gram.")

		Payload(ServeOpenAPIv3Form)
		Result(ServeOpenAPIv3Result)

		Security(security.ByKey)
		Security(security.Session)

		HTTP(func() {
			GET("/rpc/assets.serveOpenAPIv3")
			Param("id")
			Param("project_id")

			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("content_length:Content-Length")
				Header("last_modified:Last-Modified")
			})

			security.ByKeyHeader()
			security.SessionHeader()
			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "serveOpenAPIv3")
		Meta("openapi:extension:x-speakeasy-name-override", "serveOpenAPIv3")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "serveOpenAPIv3"}`)
	})

	Method("serveFunction", func() {
		Description("Serve a Gram Functions asset from Gram.")

		Payload(ServeFunctionForm)
		Result(ServeFunctionResult)

		Security(security.ByKey)
		Security(security.Session)

		HTTP(func() {
			GET("/rpc/assets.serveFunction")
			Param("id")
			Param("project_id")

			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("content_length:Content-Length")
				Header("last_modified:Last-Modified")
			})

			security.ByKeyHeader()
			security.SessionHeader()
			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "serveFunction")
		Meta("openapi:extension:x-speakeasy-name-override", "serveFunction")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "serveFunction"}`)
	})

	Method("listAssets", func() {
		Description("List all assets for a project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			security.ByKeyPayload()
		})

		Result(ListAssetsResult)

		HTTP(func() {
			GET("/rpc/assets.list")
			security.SessionHeader()
			security.ProjectHeader()
			security.ByKeyHeader()
		})

		Meta("openapi:operationId", "listAssets")
		Meta("openapi:extension:x-speakeasy-name-override", "listAssets")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "ListAssets"}`)
	})

	Method("uploadChatAttachment", func() {
		Description("Upload a chat attachment to Gram.")

		Security(security.ByKey, security.ProjectSlug, func() {
			Scope("producer")
		})
		Security(security.Session, security.ProjectSlug)
		Security(security.ChatSessionsToken, security.ProjectSlug)

		Payload(UploadChatAttachmentForm)

		Result(UploadChatAttachmentResult)

		HTTP(func() {
			POST("/rpc/assets.uploadChatAttachment")
			Header("content_type:Content-Type")
			Header("content_length:Content-Length")
			security.ByKeyHeader()
			security.ProjectHeader()
			security.SessionHeader()
			security.ChatSessionsTokenHeader()
			SkipRequestBodyEncodeDecode()
		})

		Meta("openapi:operationId", "uploadChatAttachment")
		Meta("openapi:extension:x-speakeasy-name-override", "uploadChatAttachment")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "UploadChatAttachment"}`)
	})

	Method("serveChatAttachment", func() {
		Description("Serve a chat attachment from Gram.")

		Payload(ServeChatAttachmentForm)
		Result(ServeChatAttachmentResult)

		Security(security.ByKey)
		Security(security.Session)
		Security(security.ChatSessionsToken)

		HTTP(func() {
			GET("/rpc/assets.serveChatAttachment")
			Param("id")
			Param("project_id")

			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("content_length:Content-Length")
				Header("last_modified:Last-Modified")
			})

			security.ByKeyHeader()
			security.SessionHeader()
			security.ChatSessionsTokenHeader()
			SkipResponseBodyEncodeDecode()
		})

		Meta("openapi:operationId", "serveChatAttachment")
		Meta("openapi:extension:x-speakeasy-name-override", "serveChatAttachment")
		Meta("openapi:extension:x-speakeasy-react-hook", `{"name": "serveChatAttachment"}`)
	})

})

var ListAssetsResult = Type("ListAssetsResult", func() {
	Required("assets")

	Attribute("assets", ArrayOf(Asset), "The list of assets")
})

var ServeImageForm = Type("ServeImageForm", func() {
	Required("id")
	Attribute("id", String, "The ID of the asset to serve")
})

var ServeImageResult = Type("ServeImageResult", func() {
	Required("content_type", "content_length", "last_modified")

	Attribute("content_type", String)
	Attribute("content_length", Int64)
	Attribute("last_modified", String)
	Attribute("access_control_allow_origin", String)
})

var UploadOpenAPIv3Form = Type("UploadOpenAPIv3Form", func() {
	Required("content_type", "content_length")
	security.ByKeyPayload()
	security.SessionPayload()
	security.ProjectPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
})

var FetchOpenAPIv3FromURLForm = Type("FetchOpenAPIv3FromURLForm", func() {
	Required("url")
	security.ByKeyPayload()
	security.SessionPayload()
	security.ProjectPayload()

	Attribute("url", String, "The URL to fetch the OpenAPI document from")
})

var UploadOpenAPIv3Result = Type("UploadOpenAPIv3Result", func() {
	Required("asset")

	Attribute("asset", Asset, "The asset entry that was created in Gram")
})

var UploadImageForm = Type("UploadImageForm", func() {
	Required("content_type", "content_length")
	security.ByKeyPayload()
	security.SessionPayload()
	security.ProjectPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
})

var UploadImageResult = Type("UploadImageResult", func() {
	Required("asset")

	Attribute("asset", Asset, "The asset entry that was created in Gram")
})

var UploadFunctionsForm = Type("UploadFunctionsForm", func() {
	Required("content_type", "content_length")
	security.ByKeyPayload()
	security.SessionPayload()
	security.ProjectPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
})

var UploadFunctionsResult = Type("UploadFunctionsResult", func() {
	Required("asset")

	Attribute("asset", Asset, "The asset entry that was created in Gram")
})

var ServeOpenAPIv3Form = Type("ServeOpenAPIv3Form", func() {
	Required("id", "project_id")

	security.ByKeyPayload()
	security.SessionPayload()

	Attribute("id", String, "The ID of the asset to serve")
	Attribute("project_id", String, "The procect ID that the asset belongs to")
})

var ServeOpenAPIv3Result = Type("ServeOpenAPIv3Result", func() {
	Required("content_type", "content_length", "last_modified")

	Attribute("content_type", String)
	Attribute("content_length", Int64)
	Attribute("last_modified", String)
})

var ServeFunctionForm = Type("ServeFunctionForm", func() {
	Required("id", "project_id")

	security.ByKeyPayload()
	security.SessionPayload()

	Attribute("id", String, "The ID of the asset to serve")
	Attribute("project_id", String, "The procect ID that the asset belongs to")
})

var ServeFunctionResult = Type("ServeFunctionResult", func() {
	Required("content_type", "content_length", "last_modified")

	Attribute("content_type", String)
	Attribute("content_length", Int64)
	Attribute("last_modified", String)
})

var Asset = Type("Asset", func() {
	Required("id", "kind", "sha256", "content_type", "content_length", "created_at", "updated_at")

	Attribute("id", String, "The ID of the asset")
	Attribute("kind", String, func() {
		Enum("openapiv3", "image", "functions", "chat_attachment", "unknown")
	})
	Attribute("sha256", String, "The SHA256 hash of the asset")
	Attribute("content_type", String, "The content type of the asset")
	Attribute("content_length", Int64, "The content length of the asset")
	Attribute("created_at", String, func() {
		Description("The creation date of the asset.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the asset.")
		Format(FormatDateTime)
	})
})

var UploadChatAttachmentForm = Type("UploadChatAttachmentForm", func() {
	Required("content_type", "content_length")
	security.ByKeyPayload()
	security.SessionPayload()
	security.ProjectPayload()
	security.ChatSessionsTokenPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
})

var UploadChatAttachmentResult = Type("UploadChatAttachmentResult", func() {
	Required("asset", "url")

	Attribute("asset", Asset, "The asset entry that was created in Gram")
	Attribute("url", String, "The URL to serve the chat attachment")
})

var ServeChatAttachmentForm = Type("ServeChatAttachmentForm", func() {
	Required("id", "project_id")
	security.ByKeyPayload()
	security.SessionPayload()
	security.ChatSessionsTokenPayload()

	Attribute("id", String, "The ID of the attachment to serve")
	Attribute("project_id", String, "The project ID that the attachment belongs to")
})

var ServeChatAttachmentResult = Type("ServeChatAttachmentResult", func() {
	Required("content_type", "content_length", "last_modified")

	Attribute("content_type", String)
	Attribute("content_length", Int64)
	Attribute("last_modified", String)
})
