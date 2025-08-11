package assets

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("assets", func() {
	Description("Manages assets used by Gram projects.")

	Security(security.ByKey, security.ProjectSlug, func() {
		Scope("producer")
	})
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("serveImage", func() {
		Description("Serve an image from Gram.")
		Security(security.ByKey)
		Security(security.Session)

		Payload(ServeImageForm)
		Result(ServeImageResult)

		HTTP(func() {
			GET("/rpc/assets.serveImage")
			Param("id")

			Response(StatusOK, func() {
				Header("content_type:Content-Type")
				Header("content_length:Content-Length")
				Header("last_modified:Last-Modified")
			})

			security.SessionHeader()
			security.ByKeyHeader()
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

	Method("serveOpenAPIv3", func() {
		Description("Serve an OpenAPIv3 asset from Gram.")

		Security(security.ByKey)
		Security(security.Session)

		Payload(ServeOpenAPIv3Form)
		Result(ServeOpenAPIv3Result)

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

})

var ListAssetsResult = Type("ListAssetsResult", func() {
	Required("assets")

	Attribute("assets", ArrayOf(Asset), "The list of assets")
})

var ServeImageForm = Type("ServeImageForm", func() {
	Required("id")
	security.SessionPayload()
	security.ByKeyPayload()

	Attribute("id", String, "The ID of the asset to serve")
})

var ServeImageResult = Type("ServeImageResult", func() {
	Required("content_type", "content_length", "last_modified")

	Attribute("content_type", String)
	Attribute("content_length", Int64)
	Attribute("last_modified", String)
})

var UploadOpenAPIv3Form = Type("UploadOpenAPIv3Form", func() {
	Required("content_type", "content_length")
	security.ByKeyPayload()
	security.SessionPayload()
	security.ProjectPayload()

	Attribute("content_type", String)
	Attribute("content_length", Int64)
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

var Asset = Type("Asset", func() {
	Required("id", "kind", "sha256", "content_type", "content_length", "created_at", "updated_at")

	Attribute("id", String, "The ID of the asset")
	Attribute("kind", String, func() {
		Enum("openapiv3", "image", "unknown")
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
