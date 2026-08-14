package audit

// SpeakeasyTeamActorLabel is the collective name a Speakeasy staff action
// carries in a customer's audit feed, instead of the staff member's own email.
//
// It has two callers and they work in opposite directions. auditapi applies it
// on read, masking an actor it recognises as a member of the Speakeasy
// organization. A writer that already knows it is acting as staff inside a
// customer's organization applies it at write time instead, because that mask
// only fires for an actor id it can match against a Gram user, and a writer
// authenticated somewhere other than Gram (the admin app's OIDC subject, for
// one) has no such id. Both must produce the same string, or the same action
// reads two different ways depending on which path recorded it, so there is one
// definition here rather than a literal at each site.
const SpeakeasyTeamActorLabel = "Speakeasy Team"
