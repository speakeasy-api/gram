package constants

// DemoOrganizationID is the fixed id of the shared read-only demo
// organization provisioned by seed/demo/. Impersonation-time carve-outs
// (e.g. opening chat transcripts) key off this id rather than
// organization_metadata.gram_account_type, which other flows overwrite.
const DemoOrganizationID = "org_gram_demo_workspace"
