---
"server": patch
---

Internal: adds the nullable `project_marketplace_settings.observability_enabled` column, which will let a project opt out of publishing and installing its observability plugin. Nothing reads it in this release — `NULL` means enabled, the behavior every project has today — and the follow-up wires the setting into the publish, device-agent, and dashboard paths.
