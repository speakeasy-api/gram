---
"server": minor
"dashboard": minor
---

Retires the Setup board now that the onboarding wizard has replaced it. The `organizations.listSetupTasks` and `organizations.updateSetupTask` endpoints, the setup task catalog, the setup task assignment email and the Setup board, wizard and task pages are removed, along with the `gram-new-onboarding` rollout flag: every organization is on the onboarding wizard. The sidebar, org home and the identity page point at onboarding or the WorkOS portal instead, and the enterprise admin onboarding email and the WorkOS setup callback land on the wizard. The `organization_setup_tasks` table is dropped in a follow-up migration.
