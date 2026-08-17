---
"server": minor
---

Add organization-tier and platform-tier asset upload endpoints. `organizationAssets.uploadImage` lets org admins upload images owned by the organization (for example remote identity provider logos), and `adminAssets.uploadImage` lets platform admins upload platform-wide images. Project-tier asset writes now also record the owning `organization_id` (dual-write), including repairing it on upsert conflicts, ahead of a manual backfill of existing rows.
