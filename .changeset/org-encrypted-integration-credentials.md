---
"server": patch
---

Moves Slack workspace, device integration and network ingress credentials to organization encryption, behind the `org-encrypted-integration-credentials` flag. For an enrolled organization every write also seals the credential under the organization's own key, reads use that copy and fail closed when it does not open, and an hourly backfill seals credentials written before enrolment. The application-key copy keeps being written for every organization, so turning the flag off rolls an organization back; those copies are removed once every organization reads its own.
