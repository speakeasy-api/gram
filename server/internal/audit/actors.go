package audit

// SpeakeasyTeamActorLabel is the collective name a Speakeasy staff action
// carries in a customer's audit feed, instead of the staff member's email.
//
// auditapi applies it on read, by actor id. A writer authenticated outside Gram
// has no actor id to match, so it applies the label itself. One constant keeps
// the two paths from drifting.
const SpeakeasyTeamActorLabel = "Speakeasy Team"
