// Package threadsource defines the source surfaces an assistant thread can
// originate from. It is a dependency-free leaf shared by the systems that key
// off a thread's source kind — trigger definitions, the assistants ingress
// adapters, and memory provenance — as a forcing function to keep them in
// lockstep when a surface is added or renamed.
package threadsource

// Source kinds stamped on assistant_threads.source_kind. The set is an open
// enum: the column is TEXT with no database constraint, so consumers must
// tolerate kinds they do not know about (typically by recording or relaying
// them verbatim) rather than rejecting them.
const (
	KindSlack     = "slack"
	KindCron      = "cron"
	KindWake      = "wake"
	KindDashboard = "dashboard"
)
