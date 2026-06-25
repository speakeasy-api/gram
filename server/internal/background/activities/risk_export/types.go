// Package risk_export holds the activities for the governed risk-clustering
// export workflow. Every query runs read-only against the replica pool and the
// output is written to object storage (or a local directory in dev).
package risk_export

import (
	"time"

	"github.com/google/uuid"
)

// Mode selects the export read-model shape.
type Mode string

const (
	// ModeFindingCentric emits one record per active finding plus the
	// surrounding turn window (reuses the ListRiskWindowedMessages idiom).
	ModeFindingCentric Mode = "finding_centric"
	// ModeFullTranscript emits every message in each chat joined to its active
	// findings.
	ModeFullTranscript Mode = "full_transcript"
)

// Filters mirrors the export read-model predicate. Nil pointers and empty
// slices mean "no constraint on this dimension".
type Filters struct {
	OrganizationID  string
	ProjectID       *uuid.UUID
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
	ExternalUserID  *string
	RiskPolicyID    *uuid.UUID
	RuleIDs         []string
	Sources         []string
	Severities      []string
	Roles           []string
	Models          []string
	MsgSources      []string
	HasFindingsOnly bool
}

// Sampling controls deterministic chat-level uniform sampling. A fixed
// (Filters, Seed, Percent) triple reproduces the same keep-set. Percent >= 100
// disables sampling.
type Sampling struct {
	Percent int32
	Seed    int64
}

// CountExportRowsArgs counts the sampled chat population for the dry-run gate
// and the audit record.
type CountExportRowsArgs struct {
	Filters  Filters
	Sampling Sampling
}

type CountExportRowsResult struct {
	ChatCount int64
}

// FetchExportChatPageArgs fetches one keyset page of sampled chat IDs.
type FetchExportChatPageArgs struct {
	Filters  Filters
	Sampling Sampling
	AfterID  *uuid.UUID
	PageSize int32
}

type FetchExportChatPageResult struct {
	ChatIDs []uuid.UUID
	LastID  *uuid.UUID
	HasMore bool
}

// WriteExportChunkArgs extracts and writes one part file for a batch of chats.
type WriteExportChunkArgs struct {
	Mode         Mode
	Filters      Filters
	ContextSize  int64
	ChatIDs      []uuid.UUID
	OutputPrefix string // resolved base, e.g. "risk-exports/<env>/<org>/<request_id>"
	PartIndex    int
	TargetKind   string // "gcs" | "local"
}

type WriteExportChunkResult struct {
	ObjectPath string
	RowCount   int64
	ChatCount  int
}

// Manifest is the run summary written alongside the part files. It carries no
// message content, only counts and provenance, so it is safe to surface in the
// audit record.
type Manifest struct {
	RequestID      string    `json:"request_id"`
	Operator       string    `json:"operator"`
	Mode           Mode      `json:"mode"`
	ContextSize    int64     `json:"context_size"`
	SamplePercent  int32     `json:"sample_percent"`
	SampleSeed     int64     `json:"sample_seed"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      *string   `json:"project_id,omitempty"`
	UsedReplica    bool      `json:"used_replica"`
	ReplicaReadAt  time.Time `json:"replica_read_at"`
	TotalChats     int64     `json:"total_chats"`
	TotalRows      int64     `json:"total_rows"`
	Parts          []string  `json:"parts"`
	SchemaVersion  int       `json:"schema_version"`
}

// FinalizeExportArgs writes the manifest and presigns the retrieval URL.
type FinalizeExportArgs struct {
	OutputPrefix string
	TargetKind   string
	SignedTTL    time.Duration
	Manifest     Manifest
}

type FinalizeExportResult struct {
	ManifestObjectPath string
	ManifestSignedURL  string
}
