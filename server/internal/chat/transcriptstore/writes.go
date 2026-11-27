package transcriptstore

import (
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore/internal/plainrepo"
)

// UpsertCorrelatedChatMessageParams is one correlated message to insert or
// promote with UpsertCorrelatedChatMessage. Its shape comes from the
// internal/plainrepo query of the same name.
type UpsertCorrelatedChatMessageParams = plainrepo.UpsertCorrelatedChatMessageParams

// UpsertCorrelatedChatMessageRow is the stored message after the upsert.
type UpsertCorrelatedChatMessageRow = plainrepo.UpsertCorrelatedChatMessageRow
