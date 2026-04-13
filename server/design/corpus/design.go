package corpus

import (
	"github.com/speakeasy-api/gram/server/design/security"
	"github.com/speakeasy-api/gram/server/design/shared"
	. "goa.design/goa/v3/dsl"
)

var _ = Service("corpus", func() {
	Description("Manages content corpus drafts and publishing.")
	Security(security.Session, security.ProjectSlug)
	shared.DeclareErrorResponses()

	Method("createDraft", func() {
		Description("Create a new draft.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("file_path", "operation")
			Attribute("file_path", String, "Path of the file in the corpus")
			Attribute("title", String, "Title of the draft")
			Attribute("content", String, "Content body")
			Attribute("original_content", String, "Original content before changes")
			Attribute("operation", String, func() {
				Description("The operation type")
				Enum("create", "update", "delete")
			})
			Attribute("source", String, "Source of the draft")
			Attribute("author_type", String, "Type of the author")
			Attribute("author_user_id", String, "User ID of the author")
			Attribute("agent_name", String, "Name of the agent that authored the draft")
			Attribute("labels", ArrayOf(String), "Labels for the draft")
		})

		Result(DraftResult)

		HTTP(func() {
			POST("/rpc/corpus.createDraft")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createDraft")
	})

	Method("getDraft", func() {
		Description("Get a draft by ID.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("id")
			Attribute("id", String, "The draft ID")
		})

		Result(DraftResult)

		HTTP(func() {
			POST("/rpc/corpus.getDraft")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getDraft")
	})

	Method("listDrafts", func() {
		Description("List drafts for a project, with optional status filter.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("status", String, "Filter by draft status")
		})

		Result(ListDraftsResult)

		HTTP(func() {
			POST("/rpc/corpus.listDrafts")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listDrafts")
	})

	Method("updateDraft", func() {
		Description("Update a draft's content.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("id", "content")
			Attribute("id", String, "The draft ID")
			Attribute("content", String, "Updated content body")
		})

		Result(DraftResult)

		HTTP(func() {
			POST("/rpc/corpus.updateDraft")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "updateDraft")
	})

	Method("deleteDraft", func() {
		Description("Soft-delete a draft.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("id")
			Attribute("id", String, "The draft ID")
		})

		Result(DraftResult)

		HTTP(func() {
			POST("/rpc/corpus.deleteDraft")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteDraft")
	})

	Method("publishDrafts", func() {
		Description("Publish one or more drafts.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("draft_ids")
			Attribute("draft_ids", ArrayOf(String), "List of draft IDs to publish")
		})

		Result(PublishResult)

		HTTP(func() {
			POST("/rpc/corpus.publishDrafts")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "publishDrafts")
	})

	Method("getEnrichments", func() {
		Description("Get open draft counts per file.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(EnrichmentsResult)

		HTTP(func() {
			POST("/rpc/corpus.getEnrichments")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getEnrichments")
	})

	Method("getFeedback", func() {
		Description("Get aggregated feedback for a corpus file.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("file_path")
			Attribute("file_path", String, "Path of the file in the corpus")
		})

		Result(FeedbackResult)

		HTTP(func() {
			POST("/rpc/corpus.getFeedback")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getFeedback")
	})

	Method("voteFeedback", func() {
		Description("Vote on feedback for a corpus file.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("file_path", "direction")
			Attribute("file_path", String, "Path of the file in the corpus")
			Attribute("direction", String, "Vote direction", func() {
				Enum("up", "down")
			})
		})

		Result(FeedbackResult)

		HTTP(func() {
			POST("/rpc/corpus.voteFeedback")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "voteFeedback")
	})

	Method("listComments", func() {
		Description("List comments for a corpus file.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("file_path")
			Attribute("file_path", String, "Path of the file in the corpus")
		})

		Result(ListCommentsResult)

		HTTP(func() {
			POST("/rpc/corpus.listComments")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listComments")
	})

	Method("addComment", func() {
		Description("Add a comment to a corpus file.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("file_path", "content")
			Attribute("file_path", String, "Path of the file in the corpus")
			Attribute("content", String, "Comment content")
		})

		Result(FeedbackCommentResult)

		HTTP(func() {
			POST("/rpc/corpus.addComment")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "addComment")
	})

	Method("listAnnotations", func() {
		Description("List annotations for a corpus file.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("file_path")
			Attribute("file_path", String, "Path of the file in the corpus")
		})

		Result(ListAnnotationsResult)

		HTTP(func() {
			POST("/rpc/corpus.listAnnotations")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "listAnnotations")
	})

	Method("createAnnotation", func() {
		Description("Create an annotation on a corpus file.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("file_path", "content")
			Attribute("file_path", String, "Path of the file in the corpus")
			Attribute("content", String, "Annotation content")
			Attribute("line_start", Int32, "Start line for the annotation")
			Attribute("line_end", Int32, "End line for the annotation")
		})

		Result(AnnotationResult)

		HTTP(func() {
			POST("/rpc/corpus.createAnnotation")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "createAnnotation")
	})

	Method("deleteAnnotation", func() {
		Description("Delete an annotation by ID.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("id")
			Attribute("id", String, "The annotation ID")
		})

		Result(AnnotationResult)

		HTTP(func() {
			POST("/rpc/corpus.deleteAnnotation")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "deleteAnnotation")
	})

	Method("getAutoPublishConfig", func() {
		Description("Get the auto-publish configuration for a project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(AutoPublishConfigResult)

		HTTP(func() {
			POST("/rpc/corpus.getAutoPublishConfig")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "getAutoPublishConfig")
	})

	Method("setAutoPublishConfig", func() {
		Description("Set the auto-publish configuration for a project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Required("enabled", "interval_minutes", "min_upvotes", "min_age_hours")
			Attribute("enabled", Boolean, "Whether auto-publish is enabled")
			Attribute("interval_minutes", Int32, "Auto-publish interval in minutes")
			Attribute("min_upvotes", Int32, "Minimum upvotes required")
			Attribute("author_type_filter", String, "Optional author type filter")
			Attribute("label_filter", ArrayOf(String), "Optional label filter")
			Attribute("min_age_hours", Int32, "Minimum draft age in hours")
		})

		Result(AutoPublishConfigResult)

		HTTP(func() {
			POST("/rpc/corpus.setAutoPublishConfig")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "setAutoPublishConfig")
	})

	Method("searchLogs", func() {
		Description("Search corpus search logs for a project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
			Attribute("cursor", String, "Cursor for pagination")
			Attribute("limit", Int, "Number of logs to return", func() {
				Minimum(1)
				Maximum(100)
				Default(20)
			})
		})

		Result(CorpusSearchLogsResult)

		HTTP(func() {
			POST("/rpc/corpus.searchLogs")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "corpusSearchLogs")
	})

	Method("searchStats", func() {
		Description("Get aggregated corpus search statistics for a project.")

		Payload(func() {
			security.SessionPayload()
			security.ProjectPayload()
		})

		Result(CorpusSearchStatsResult)

		HTTP(func() {
			POST("/rpc/corpus.searchStats")
			security.SessionHeader()
			security.ProjectHeader()
			Response(StatusOK)
		})

		Meta("openapi:operationId", "corpusSearchStats")
	})
})

var DraftResult = Type("CorpusDraftResult", func() {
	Required("id", "project_id", "file_path", "operation", "status", "created_at", "updated_at")

	Attribute("id", String, "The draft ID")
	Attribute("project_id", String, "The project ID")
	Attribute("file_path", String, "Path of the file in the corpus")
	Attribute("title", String, "Title of the draft")
	Attribute("content", String, "Content body")
	Attribute("original_content", String, "Original content before changes")
	Attribute("operation", String, func() {
		Description("The operation type")
		Enum("create", "update", "delete")
	})
	Attribute("status", String, func() {
		Description("The status of the draft")
		Enum("open", "published", "rejected")
	})
	Attribute("source", String, "Source of the draft")
	Attribute("author_type", String, "Type of the author")
	Attribute("author_user_id", String, "User ID of the author")
	Attribute("agent_name", String, "Name of the agent that authored the draft")
	Attribute("labels", ArrayOf(String), "Labels for the draft")
	Attribute("commit_sha", String, "The commit SHA if published")
	Attribute("created_at", String, func() {
		Description("The creation date of the draft.")
		Format(FormatDateTime)
	})
	Attribute("updated_at", String, func() {
		Description("The last update date of the draft.")
		Format(FormatDateTime)
	})
})

var ListDraftsResult = Type("ListCorpusDraftsResult", func() {
	Required("drafts")
	Attribute("drafts", ArrayOf(DraftResult), "The list of drafts")
})

var PublishResult = Type("PublishCorpusDraftsResult", func() {
	Required("commit_sha")
	Attribute("commit_sha", String, "The commit SHA of the published changes")
})

var EnrichmentEntry = Type("CorpusEnrichmentEntry", func() {
	Required("file_path", "open_drafts")
	Attribute("file_path", String, "Path of the file in the corpus")
	Attribute("open_drafts", Int, "Number of open drafts for this file")
})

var EnrichmentsResult = Type("CorpusEnrichmentsResult", func() {
	Required("enrichments")
	Attribute("enrichments", ArrayOf(EnrichmentEntry), "Enrichment entries per file")
})

var FeedbackResult = Type("CorpusFeedbackResult", func() {
	Required("upvotes", "downvotes", "labels")
	Attribute("upvotes", Int, "Number of upvotes")
	Attribute("downvotes", Int, "Number of downvotes")
	Attribute("labels", ArrayOf(String), "Feedback labels")
	Attribute("user_vote", String, "Current user's vote direction", func() {
		Enum("up", "down")
	})
})

var FeedbackCommentResult = Type("CorpusFeedbackCommentResult", func() {
	Required("id", "author", "author_type", "content", "created_at", "upvotes", "downvotes")
	Attribute("id", String, "The comment ID")
	Attribute("author", String, "Comment author")
	Attribute("author_type", String, "Type of author", func() {
		Enum("human", "agent")
	})
	Attribute("content", String, "Comment content")
	Attribute("created_at", String, "The creation date of the comment", func() {
		Format(FormatDateTime)
	})
	Attribute("upvotes", Int, "Comment upvotes")
	Attribute("downvotes", Int, "Comment downvotes")
})

var ListCommentsResult = Type("ListCorpusCommentsResult", func() {
	Required("comments")
	Attribute("comments", ArrayOf(FeedbackCommentResult), "Comments for the file")
})

var AnnotationResult = Type("CorpusAnnotationResult", func() {
	Required("id", "author", "author_type", "content", "created_at")
	Attribute("id", String, "The annotation ID")
	Attribute("author", String, "Annotation author")
	Attribute("author_type", String, "Type of author", func() {
		Enum("human", "agent")
	})
	Attribute("content", String, "Annotation content")
	Attribute("line_start", Int32, "Start line for the annotation")
	Attribute("line_end", Int32, "End line for the annotation")
	Attribute("created_at", String, "The creation date of the annotation", func() {
		Format(FormatDateTime)
	})
})

var ListAnnotationsResult = Type("ListCorpusAnnotationsResult", func() {
	Required("annotations")
	Attribute("annotations", ArrayOf(AnnotationResult), "Annotations for the file")
})

var AutoPublishConfigResult = Type("CorpusAutoPublishConfigResult", func() {
	Required("enabled", "interval_minutes", "min_upvotes", "min_age_hours")
	Attribute("enabled", Boolean, "Whether auto-publish is enabled")
	Attribute("interval_minutes", Int32, "Auto-publish interval in minutes")
	Attribute("min_upvotes", Int32, "Minimum upvotes required")
	Attribute("author_type_filter", String, "Optional author type filter")
	Attribute("label_filter", ArrayOf(String), "Optional label filter")
	Attribute("min_age_hours", Int32, "Minimum draft age in hours")
})

var CorpusSearchLogResult = Type("CorpusSearchLogResult", func() {
	Required("id", "project_id", "query", "filters", "result_count", "latency_ms", "session_id", "agent", "timestamp")
	Attribute("id", String, "Search log ID")
	Attribute("project_id", String, "The project ID")
	Attribute("query", String, "The search query")
	Attribute("filters", Any, "Search filters")
	Attribute("result_count", Int, "Result count")
	Attribute("latency_ms", Float64, "Search latency in milliseconds")
	Attribute("session_id", String, "Session ID")
	Attribute("agent", String, "Agent name")
	Attribute("timestamp", String, "Search timestamp", func() {
		Format(FormatDateTime)
	})
})

var CorpusSearchLogsResult = Type("CorpusSearchLogsResult", func() {
	Required("logs")
	Attribute("logs", ArrayOf(CorpusSearchLogResult), "Search logs")
	Attribute("next_cursor", String, "Cursor for the next page")
})

var CorpusQueryFrequencyResult = Type("CorpusQueryFrequencyResult", func() {
	Required("query", "count")
	Attribute("query", String, "The query string")
	Attribute("count", Int64, "How often the query was searched")
})

var CorpusSearchStatsResult = Type("CorpusSearchStatsResult", func() {
	Required("top_queries", "latency_p50", "latency_p95", "latency_p99", "total_events")
	Attribute("top_queries", ArrayOf(CorpusQueryFrequencyResult), "Top queries by frequency")
	Attribute("latency_p50", Float64, "P50 latency in milliseconds")
	Attribute("latency_p95", Float64, "P95 latency in milliseconds")
	Attribute("latency_p99", Float64, "P99 latency in milliseconds")
	Attribute("total_events", Int64, "Total number of search events")
})
