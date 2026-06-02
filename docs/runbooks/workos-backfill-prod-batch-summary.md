# WorkOS Backfill Prod Summary

This summarizes the corrected production organization preflight after global roles were created in prod.

## Overall Summary

This rollup covers all 10 organization batches, including the final 830-org batch.

### Batch Totals

| Area                          |  Count |
| ----------------------------- | -----: |
| Batches reviewed              |     10 |
| WorkOS organizations scanned  |  9,830 |
| Organizations skipped         |      2 |
| Total planned changed records | 12,862 |
| Deletes planned               |      0 |

### Entity Totals

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                         |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | --------------------------------------------------------------------------------------------- |
| Organizations      |    677 |  8,814 |      0 |   337 |          2 | Most updates are identity-linking: backfilling `workos_id` and `workos_updated_at`.           |
| Organization roles |     18 |      0 |      0 |     0 |          0 | Five roles in batch 1, thirteen roles in the final batch.                                     |
| Users              |    652 |    464 |      0 |     0 |        163 | User writes are concentrated in batch 1 and the final batch.                                  |
| Memberships        |    730 |    386 |      0 |     0 |        163 | Membership writes track the same WorkOS membership set as users.                              |
| Role assignments   |  1,115 |      0 |      0 |     1 |        163 | Global roles are present, so expected assignments are planned creates instead of stale skips. |

### Batch Overview

| Batch | WorkOS orgs | Planned changed records | Org creates | Org updates | User writes | Membership writes | Assignment creates | Notes                                                                |
| ----- | ----------: | ----------------------: | ----------: | ----------: | ----------: | ----------------: | -----------------: | -------------------------------------------------------------------- |
| 1     |       1,000 |                   3,411 |          53 |         680 |         891 |               891 |                891 | Main user, membership, and assignment backfill batch.                |
| 2     |       1,000 |                   1,002 |           2 |         997 |           1 |                 1 |                  1 | Mostly organization WorkOS-link repair.                              |
| 3     |       1,000 |                   1,000 |           7 |         993 |           0 |                 0 |                  0 | Organization WorkOS-link repair only.                                |
| 4     |       1,000 |                   1,004 |          14 |         984 |           2 |                 2 |                  2 | Organization link repair plus two user/membership/assignment writes. |
| 5     |       1,000 |                     998 |         111 |         887 |           0 |                 0 |                  0 | Organization creates and WorkOS-link repair only.                    |
| 6     |       1,000 |                   1,004 |          94 |         904 |           2 |                 2 |                  2 | Organization link repair plus two user/membership/assignment writes. |
| 7     |       1,000 |                     998 |         111 |         887 |           0 |                 0 |                  0 | Organization creates and WorkOS-link repair only.                    |
| 8     |       1,000 |                   1,002 |          59 |         940 |           1 |                 1 |                  1 | Organization link repair plus one user/membership/assignment write.  |
| 9     |       1,000 |                   1,002 |          58 |         935 |           1 |                 1 |                  1 | Organization link repair plus one user/membership/assignment write.  |
| Final |         830 |                   1,441 |         168 |         607 |         218 |               218 |                217 | Largest org-create batch; also creates 13 organization roles.        |

### Overall Review Focus

- Review the 677 organization creates before allowing writes that create new local organization rows.
- Review the 8,814 organization updates, which are mostly `workos_id` and `workos_updated_at` backfills.
- Review the 18 organization role creates and 1,115 role assignment creates for RBAC impact.
- No deletes are planned in any batch.

## Organization Batch 1

### Overview

| Area                              | Count |
| --------------------------------- | ----: |
| WorkOS organizations scanned      | 1,000 |
| Organizations skipped             |     2 |
| Organization rows affected        |   733 |
| Organization rows to create       |    53 |
| Organization rows to update       |   680 |
| Organization rows already current |   265 |
| Organization roles to create      |     5 |
| Users to create                   |   526 |
| Users to update                   |   365 |
| Memberships to create             |   601 |
| Memberships to update             |   290 |
| Role assignments to create        |   891 |
| Total planned changed records     | 3,411 |

No deletes are planned. Global roles are now present, so role assignments are no longer stale-skipped: all 891 expected assignment rows are planned creates.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                               |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | --------------------------------------------------------------------------------------------------- |
| Organizations      |     53 |    680 |      0 |   265 |          2 | Most organization writes update existing rows with WorkOS metadata; some also change display names. |
| Organization roles |      5 |      0 |      0 |     0 |          0 | Creates five WorkOS-backed organization roles.                                                      |
| Users              |    526 |    365 |      0 |     0 |          0 | Creates missing users and backfills WorkOS IDs/timestamps onto existing users.                      |
| Memberships        |    601 |    290 |      0 |     0 |          0 | Creates or repairs WorkOS membership links.                                                         |
| Role assignments   |    891 |      0 |      0 |     0 |          0 | Creates assignments now that global roles exist locally.                                            |

### Sample Organizations

| WorkOS organization    | Gram organization                      | Planned impact                                                                 |
| ---------------------- | -------------------------------------- | ------------------------------------------------------------------------------ |
| Apex Fintech Solutions | `2ec01ed7-ed8c-49fe-9a87-6c08b8431135` | Org no-op; create 16 users, 18 memberships, and 18 role assignments.           |
| ConductorOne           | `b233695c-bae6-46e3-866b-024ab950a547` | Update org; create 1 user, 1 membership, and 1 role assignment.                |
| Speakeasy              | `5a25158b-24dc-4d49-b03d-e85acfbea59c` | Update org; create 2 roles, 14 users, 33 memberships, and 58 role assignments. |
| SolarWinds             | `9b9492ec-26e7-4f62-861a-fec61b929659` | Org no-op; create 4 users, 4 memberships, and 10 role assignments.             |
| Zoom                   | Skipped                                | No local row and no usable WorkOS external ID.                                 |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity            | Action | Sample count | Risk          | Fields                                                                                            |
| ----------------- | ------ | -----------: | ------------- | ------------------------------------------------------------------------------------------------- |
| User              | Create |           24 | Identity      | `display_name`, `email`, `id`, `photo_url`, `workos_created_at`, `workos_id`, `workos_updated_at` |
| Membership        | Create |           19 | Identity      | `organization_id`, `user_id`, `workos_membership_id`, `workos_updated_at`, `workos_user_id`       |
| Role assignment   | Create |           19 | Identity      | `organization_id`, `user_id`, `workos_membership_id`, `workos_slug`                               |
| User              | Update |           14 | Identity      | `workos_created_at`, `workos_id`, `workos_updated_at`                                             |
| Organization role | Create |            2 | Identity      | `workos_created_at`, `workos_description`, `workos_name`, `workos_slug`, `workos_updated_at`      |
| User              | Update |            2 | Identity      | `photo_url`, `workos_created_at`, `workos_id`, `workos_updated_at`                                |
| Organization      | Update |            2 | Display       | `name`, `workos_updated_at`                                                                       |
| User              | Update |           17 | Metadata only | `workos_created_at`, `workos_updated_at`                                                          |
| Organization      | Update |            1 | Metadata only | `workos_updated_at`                                                                               |

### Notable Samples

| Change                           | Example                           | Notes                                                                                                   |
| -------------------------------- | --------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Organization metadata update     | Apex Fintech Solutions            | Backfills `workos_updated_at` only.                                                                     |
| Organization display-name update | ConductorOne                      | Changes `Conductor One` to `ConductorOne` and backfills `workos_updated_at`.                            |
| Organization display-name update | Speakeasy                         | Changes `Speakeasy Team` to `Speakeasy` and backfills `workos_updated_at`.                              |
| User create                      | `user_01K0CK7HPFJK960Z9BFZTH9G9G` | Creates local user `59f3976b-8c3c-4a67-9155-91647eb63513` for `gdogbey@apexclearing.com`.               |
| User WorkOS-link update          | `user_01KGXTSD0D851QCMZ030NW8E0H` | Backfills missing WorkOS ID and WorkOS timestamps onto an existing user.                                |
| Membership create                | `om_01KMDHQ96AETSZH2FDGM87ZXMD`   | Creates membership linking Apex Fintech Solutions to local user `5d35d56a-73bc-4911-8c86-386fbde0834b`. |
| Role assignment create           | `om_01KMDHQ96AETSZH2FDGM87ZXMD`   | Creates assignment for the same Apex membership.                                                        |
| Organization role create         | `org-secret-manager`              | Creates WorkOS-backed organization role `Secret Manager`.                                               |
| Organization role create         | `org-gtm`                         | Creates WorkOS-backed organization role `GTM`.                                                          |

### Review Focus

- Review the 680 organization updates, especially display-name changes.
- Review the 526 user creates and 365 user updates, because they touch identity/profile data.
- Review the 601 membership creates and 290 membership updates, because these establish org/user relationships.
- Review the 891 role assignment creates, now that global roles are present and assignment creation is unblocked.
- Confirm the two skipped organizations are expected to have no writable local mapping.

## Organization Batch 2

### Overview

| Area                              | Count |
| --------------------------------- | ----: |
| WorkOS organizations scanned      | 1,000 |
| Organizations skipped             |     0 |
| Organization rows affected        |   999 |
| Organization rows to create       |     2 |
| Organization rows to update       |   997 |
| Organization rows already current |     1 |
| Organization roles to create      |     0 |
| Users to update                   |     1 |
| Memberships to update             |     1 |
| Role assignments to create        |     1 |
| Total planned changed records     | 1,002 |

No deletes are planned. This batch is almost entirely organization metadata repair: 997 existing organization rows will have missing WorkOS fields backfilled.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                              |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ------------------------------------------------------------------ |
| Organizations      |      2 |    997 |      0 |     1 |          0 | Most updates backfill missing `workos_id` and `workos_updated_at`. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                          |
| Users              |      0 |      1 |      0 |     0 |          0 | One existing user will be updated.                                 |
| Memberships        |      0 |      1 |      0 |     0 |          0 | One existing membership will be updated.                           |
| Role assignments   |      1 |      0 |      0 |     0 |          0 | One role assignment will be created.                               |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| Encore              | `1d0e60b9-caa5-469c-849b-66d329e8c417` | Update org WorkOS ID and timestamp. |
| GNI                 | `3c7a7a8f-9b3d-4e45-a041-a7648b6e2e71` | Update org WorkOS ID and timestamp. |
| Gantry              | `a6c1db7d-da81-4980-a6b6-e5f3388125c5` | Update org WorkOS ID and timestamp. |
| netbird             | `541f370f-d1de-4948-ab6f-d6eb6da0214d` | Update org WorkOS ID and timestamp. |
| Noormahal           | `0e9c558d-9201-4a26-9300-7aaf819f556b` | Update org WorkOS ID and timestamp. |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk     | Fields                           |
| ------------ | ------ | -----------: | -------- | -------------------------------- |
| Organization | Update |          100 | Identity | `workos_id`, `workos_updated_at` |

### Notable Samples

| Change                          | Example | Notes                                                                                                  |
| ------------------------------- | ------- | ------------------------------------------------------------------------------------------------------ |
| Organization WorkOS-link update | Encore  | Backfills `workos_id=org_01KPZEXJMXNQG8C3SH54TP4BTS` and `workos_updated_at=2026-04-24T10:00:13.714Z`. |
| Organization WorkOS-link update | GNI     | Backfills `workos_id=org_01KPZEXK0W98CQ0VVP8Z0AT33E` and `workos_updated_at=2026-04-24T10:00:14.099Z`. |
| Organization WorkOS-link update | Gantry  | Backfills `workos_id=org_01KPZEXKC5PVS9GR24CTBRNDMZ` and `workos_updated_at=2026-04-24T10:00:14.458Z`. |

### Review Focus

- Review why 997 organizations exist locally without `workos_id`.
- Confirm the 2 organization creates are expected.
- Confirm the single user update, membership update, and role assignment create are expected.
- This batch is mostly identity-linking rather than RBAC expansion; the RBAC impact is limited to one assignment create.

## Organization Batch 3

### Overview

| Area                                 | Count |
| ------------------------------------ | ----: |
| WorkOS organizations scanned         | 1,000 |
| Organizations skipped                |     0 |
| Organization rows affected           | 1,000 |
| Organization rows to create          |     7 |
| Organization rows to update          |   993 |
| Organization rows already current    |     0 |
| Organization roles to create         |     0 |
| Users to create or update            |     0 |
| Memberships to create or update      |     0 |
| Role assignments to create or update |     0 |
| Total planned changed records        | 1,000 |

No deletes are planned. This batch is pure organization metadata repair: 993 existing organization rows will have missing WorkOS fields backfilled, and 7 organization rows will be created.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                              |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ------------------------------------------------------------------ |
| Organizations      |      7 |    993 |      0 |     0 |          0 | Most updates backfill missing `workos_id` and `workos_updated_at`. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                          |
| Users              |      0 |      0 |      0 |     0 |          0 | No user changes expected.                                          |
| Memberships        |      0 |      0 |      0 |     0 |          0 | No membership changes expected.                                    |
| Role assignments   |      0 |      0 |      0 |     0 |          0 | No assignment changes expected.                                    |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| Rutter              | `57e7f92b-7f75-415b-9ca9-18846923229d` | Update org WorkOS ID and timestamp. |
| MWS                 | `9b4b4401-5f85-4879-9b51-5cdc899712b3` | Update org WorkOS ID and timestamp. |
| Sivo-Ignite         | `31b542af-719d-463e-b4f7-fff338a3e6a7` | Update org WorkOS ID and timestamp. |
| Steve-Test          | `7a953b19-d7e9-4660-badb-2c1fba7f65ee` | Update org WorkOS ID and timestamp. |
| samdotci            | `f6af86ab-fd8e-4a6d-a202-02c57559198f` | Update org WorkOS ID and timestamp. |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk     | Fields                           |
| ------------ | ------ | -----------: | -------- | -------------------------------- |
| Organization | Update |          100 | Identity | `workos_id`, `workos_updated_at` |

### Notable Samples

| Change                          | Example     | Notes                                                                                                  |
| ------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------ |
| Organization WorkOS-link update | Rutter      | Backfills `workos_id=org_01KPZF932ZYZNT1A57AXTWJRCD` and `workos_updated_at=2026-04-24T10:06:31Z`.     |
| Organization WorkOS-link update | MWS         | Backfills `workos_id=org_01KPZF93DS1XKBJQTZGAY99FN2` and `workos_updated_at=2026-04-24T10:06:31.345Z`. |
| Organization WorkOS-link update | Sivo-Ignite | Backfills `workos_id=org_01KPZF93REM04NCA5NCTY34JFP` and `workos_updated_at=2026-04-24T10:06:31.685Z`. |

### Review Focus

- Review why 993 organizations exist locally without `workos_id`.
- Confirm the 7 organization creates are expected.
- This batch has no user, membership, role, or assignment changes, so the practical risk is identity-linking existing organizations to WorkOS records.

## Organization Batch 4

### Overview

| Area                              | Count |
| --------------------------------- | ----: |
| WorkOS organizations scanned      | 1,000 |
| Organizations skipped             |     0 |
| Organization rows affected        |   998 |
| Organization rows to create       |    14 |
| Organization rows to update       |   984 |
| Organization rows already current |     2 |
| Organization roles to create      |     0 |
| Users to create                   |     2 |
| Memberships to create             |     2 |
| Role assignments to create        |     2 |
| Total planned changed records     | 1,004 |

No deletes are planned. This batch is mostly organization metadata repair, with a very small user, membership, and role assignment surface.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                              |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ------------------------------------------------------------------ |
| Organizations      |     14 |    984 |      0 |     2 |          0 | Most updates backfill missing `workos_id` and `workos_updated_at`. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                          |
| Users              |      2 |      0 |      0 |     0 |          0 | Two missing WorkOS-linked users will be created.                   |
| Memberships        |      2 |      0 |      0 |     0 |          0 | Two memberships will be created.                                   |
| Role assignments   |      2 |      0 |      0 |     0 |          0 | Two role assignments will be created.                              |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| ensembleanalytics   | `3248eb95-49ea-4154-adf5-84c177c651fd` | Update org WorkOS ID and timestamp. |
| Fun-Forge-Studio    | `d3ef7e7d-0c05-4451-b893-b2ab21d6cd2c` | Update org WorkOS ID and timestamp. |
| Personal            | `c389e2f7-ff77-42e0-84aa-3e125f3a410a` | Update org WorkOS ID and timestamp. |
| xyz                 | `41931e16-bae0-4cd3-8460-b17d8e9b5d8a` | Update org WorkOS ID and timestamp. |
| TouchInspiration    | `35ed9fe3-6910-42a0-8fea-3464c901bd55` | Update org WorkOS ID and timestamp. |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk     | Fields                                                 |
| ------------ | ------ | -----------: | -------- | ------------------------------------------------------ |
| Organization | Update |           98 | Identity | `workos_id`, `workos_updated_at`                       |
| Organization | Create |            2 | Identity | `id`, `name`, `slug`, `workos_id`, `workos_updated_at` |

### Notable Samples

| Change                          | Example           | Notes                                                                                                              |
| ------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------ |
| Organization WorkOS-link update | ensembleanalytics | Backfills `workos_id=org_01KPZGX192HBSESMZXYRTB65NA` and `workos_updated_at=2026-04-24T10:34:53.078Z`.             |
| Organization WorkOS-link update | Fun-Forge-Studio  | Backfills `workos_id=org_01KPZGX1MATKS3R6G6A5DM7TCS` and `workos_updated_at=2026-04-24T10:34:53.44Z`.              |
| Organization WorkOS-link update | Personal          | Backfills `workos_id=org_01KPZGX203BFXVRDPFWDC7A3CR` and `workos_updated_at=2026-04-24T10:34:53.808Z`.             |
| Organization create             | todelete          | Creates local organization `4743a1a6-4618-46f2-861c-3ff73d464c98` for WorkOS org `org_01KPZGXC1WEGHSH2Y8RQYBGP78`. |
| Organization create             | panora            | Creates local organization `fee2fc7b-c0ad-4f33-8bec-b777ffbbfacd` for WorkOS org `org_01KPZGXE5M9PPPFJM11ZF5G0V7`. |

### Review Focus

- Review why 984 organizations exist locally without `workos_id`.
- Confirm the 14 organization creates are expected, especially small/test-like names.
- Confirm the two user, membership, and assignment creates are expected.
- This batch is mostly identity-linking existing organizations to WorkOS records; the RBAC impact is limited to two assignment creates.

## Organization Batch 5

### Overview

| Area                                 | Count |
| ------------------------------------ | ----: |
| WorkOS organizations scanned         | 1,000 |
| Organizations skipped                |     0 |
| Organization rows affected           |   998 |
| Organization rows to create          |   111 |
| Organization rows to update          |   887 |
| Organization rows already current    |     2 |
| Organization roles to create         |     0 |
| Users to create or update            |     0 |
| Memberships to create or update      |     0 |
| Role assignments to create or update |     0 |
| Total planned changed records        |   998 |

No deletes are planned. This batch is organization-only: 887 existing organization rows will have missing WorkOS fields backfilled, and 111 organization rows will be created.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                                                         |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ----------------------------------------------------------------------------------------------------------------------------- |
| Organizations      |    111 |    887 |      0 |     2 |          0 | Updates backfill missing `workos_id` and `workos_updated_at`; creates add local org rows for WorkOS orgs not present locally. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                                                                                     |
| Users              |      0 |      0 |      0 |     0 |          0 | No user changes expected.                                                                                                     |
| Memberships        |      0 |      0 |      0 |     0 |          0 | No membership changes expected.                                                                                               |
| Role assignments   |      0 |      0 |      0 |     0 |          0 | No assignment changes expected.                                                                                               |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| test                | `f43eaba9-5aed-4728-889d-4bc95b5359e3` | Update org WorkOS ID and timestamp. |
| brass               | `9098b59d-f203-419f-b744-939ff24f094f` | Update org WorkOS ID and timestamp. |
| abc                 | `9b3d45a3-9661-4cec-a550-dc8e5586aeda` | Update org WorkOS ID and timestamp. |
| aliceswartz         | `6e9a49fb-3e2c-4ac1-a7cd-f1f4699d89ec` | Create organization metadata.       |
| fetishplaytube      | `0055c707-9751-4164-935b-4cff65f20ca9` | Create organization metadata.       |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk     | Fields                                                 |
| ------------ | ------ | -----------: | -------- | ------------------------------------------------------ |
| Organization | Update |           87 | Identity | `workos_id`, `workos_updated_at`                       |
| Organization | Create |           13 | Identity | `id`, `name`, `slug`, `workos_id`, `workos_updated_at` |

### Notable Samples

| Change                          | Example        | Notes                                                                                                              |
| ------------------------------- | -------------- | ------------------------------------------------------------------------------------------------------------------ |
| Organization WorkOS-link update | test           | Backfills `workos_id=org_01KPZH8BQMXJF4BSNVHQP5KFYS` and `workos_updated_at=2026-04-24T10:41:04.235Z`.             |
| Organization WorkOS-link update | brass          | Backfills `workos_id=org_01KPZH8C2V2RN8Y6G6NXCS39JP` and `workos_updated_at=2026-04-24T10:41:04.595Z`.             |
| Organization WorkOS-link update | abc            | Backfills `workos_id=org_01KPZH8CDJ1SD6Z2JDFE4D5WE3` and `workos_updated_at=2026-04-24T10:41:04.934Z`.             |
| Organization create             | aliceswartz    | Creates local organization `6e9a49fb-3e2c-4ac1-a7cd-f1f4699d89ec` for WorkOS org `org_01KPZH8CSBVC41A13RBC7RQ9NQ`. |
| Organization create             | fetishplaytube | Creates local organization `0055c707-9751-4164-935b-4cff65f20ca9` for WorkOS org `org_01KPZH8D52F9T6437K7KZSEDV2`. |

### Review Focus

- Review why 887 organizations exist locally without `workos_id`.
- Review the 111 organization creates, which is much higher than prior org-only batches.
- This batch has no user, membership, role, or assignment changes, so the practical risk is limited to organization identity-linking and organization creation.

## Organization Batch 6

### Overview

| Area                              | Count |
| --------------------------------- | ----: |
| WorkOS organizations scanned      | 1,000 |
| Organizations skipped             |     0 |
| Organization rows affected        |   998 |
| Organization rows to create       |    94 |
| Organization rows to update       |   904 |
| Organization rows already current |     2 |
| Organization roles to create      |     0 |
| Users to create                   |     1 |
| Users to update                   |     1 |
| Memberships to create             |     1 |
| Memberships to update             |     1 |
| Role assignments to create        |     2 |
| Total planned changed records     | 1,004 |

No deletes are planned. This batch is mostly organization identity-linking, plus a very small user, membership, and assignment surface.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                                                         |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ----------------------------------------------------------------------------------------------------------------------------- |
| Organizations      |     94 |    904 |      0 |     2 |          0 | Updates backfill missing `workos_id` and `workos_updated_at`; creates add local org rows for WorkOS orgs not present locally. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                                                                                     |
| Users              |      1 |      1 |      0 |     0 |          0 | One user create and one user update.                                                                                          |
| Memberships        |      1 |      1 |      0 |     0 |          0 | One membership create and one membership update.                                                                              |
| Role assignments   |      2 |      0 |      0 |     0 |          0 | Two role assignments will be created.                                                                                         |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| Miza                | `4f1bef0f-60b9-4894-8dcf-a5b033bea13d` | Update org WorkOS ID and timestamp. |
| mlagast             | `0b11cd3b-6305-46e9-9c4e-ca6fb49dc856` | Update org WorkOS ID and timestamp. |
| odarino             | `5d901b7c-ca12-47a7-8a0b-a0f4bc9c579c` | Update org WorkOS ID and timestamp. |
| lambdamatt          | `b883f14c-3fa4-470e-8af4-0ebfd2e0d7a6` | Update org WorkOS ID and timestamp. |
| foo                 | `440d8b13-d7ed-4443-a2e2-94939f9bcf9f` | Update org WorkOS ID and timestamp. |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk     | Fields                                                 |
| ------------ | ------ | -----------: | -------- | ------------------------------------------------------ |
| Organization | Update |           91 | Identity | `workos_id`, `workos_updated_at`                       |
| Organization | Create |            9 | Identity | `id`, `name`, `slug`, `workos_id`, `workos_updated_at` |

### Notable Samples

| Change                          | Example  | Notes                                                                                                              |
| ------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------ |
| Organization WorkOS-link update | Miza     | Backfills `workos_id=org_01KPZHKRNEA2SMFWGDMZ8QG69Q` and `workos_updated_at=2026-04-24T10:47:17.923Z`.             |
| Organization WorkOS-link update | mlagast  | Backfills `workos_id=org_01KPZHKS0TMNK4PE8JKJ8WCRJA` and `workos_updated_at=2026-04-24T10:47:18.288Z`.             |
| Organization WorkOS-link update | odarino  | Backfills `workos_id=org_01KPZHKSCCT9Z7KW8068EKMV18` and `workos_updated_at=2026-04-24T10:47:18.66Z`.              |
| Organization create             | t        | Creates local organization `e0d39790-d65f-4c92-a20b-4afbb467bd7e` for WorkOS org `org_01KPZHKWRT4M1MW2FQANZNARWA`. |
| Organization create             | Okay     | Creates local organization `7ebac463-4cca-4c11-a1c2-975cc3fc3f2f` for WorkOS org `org_01KPZHM6V34SMNVVX1MJ3087X5`. |
| Organization create             | personal | Creates local organization `7521c38e-1443-4206-bc48-25cd36e1f685` for WorkOS org `org_01KPZHMCF4GH75Q6J12X0RCPYK`. |

### Review Focus

- Review why 904 organizations exist locally without `workos_id`.
- Review the 94 organization creates.
- Confirm the one user create, one user update, one membership create, one membership update, and two assignment creates are expected.
- This batch is mostly organization identity-linking; the RBAC impact is limited to two assignment creates.

## Organization Batch 7

### Overview

| Area                                 | Count |
| ------------------------------------ | ----: |
| WorkOS organizations scanned         | 1,000 |
| Organizations skipped                |     0 |
| Organization rows affected           |   998 |
| Organization rows to create          |   111 |
| Organization rows to update          |   887 |
| Organization rows already current    |     2 |
| Organization roles to create         |     0 |
| Users to create or update            |     0 |
| Memberships to create or update      |     0 |
| Role assignments to create or update |     0 |
| Total planned changed records        |   998 |

No deletes are planned. This batch is organization-only: 887 existing organization rows will have missing WorkOS fields backfilled, and 111 organization rows will be created.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                                                     |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ------------------------------------------------------------------------------------------------------------------------- |
| Organizations      |    111 |    887 |      0 |     2 |          0 | Most updates backfill missing `workos_id` and `workos_updated_at`; one sampled update only backfills `workos_updated_at`. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                                                                                 |
| Users              |      0 |      0 |      0 |     0 |          0 | No user changes expected.                                                                                                 |
| Memberships        |      0 |      0 |      0 |     0 |          0 | No membership changes expected.                                                                                           |
| Role assignments   |      0 |      0 |      0 |     0 |          0 | No assignment changes expected.                                                                                           |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| Foody Tech LTD      | `2ccccd74-7ba2-45a6-bd8c-5a4a62f269d8` | Update org WorkOS ID and timestamp. |
| Mateffy             | `988cd7cf-31bf-4dbe-b340-90018ec8c263` | Update org WorkOS ID and timestamp. |
| maestro             | `c4d46a37-8bf4-499e-a85d-a6145e487bbc` | Update org WorkOS ID and timestamp. |
| studiox             | `3ff76928-b90a-4302-ab26-f1ea99bdd57f` | Update org WorkOS ID and timestamp. |
| K-rks               | `e1f2a069-9ea5-444a-893c-58fd63d678cd` | Update org WorkOS ID and timestamp. |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk          | Fields                                                 |
| ------------ | ------ | -----------: | ------------- | ------------------------------------------------------ |
| Organization | Update |           96 | Identity      | `workos_id`, `workos_updated_at`                       |
| Organization | Create |            3 | Identity      | `id`, `name`, `slug`, `workos_id`, `workos_updated_at` |
| Organization | Update |            1 | Metadata only | `workos_updated_at`                                    |

### Notable Samples

| Change                          | Example         | Notes                                                                                                              |
| ------------------------------- | --------------- | ------------------------------------------------------------------------------------------------------------------ |
| Organization WorkOS-link update | Foody Tech LTD  | Backfills `workos_id=org_01KPZHZ6T80B29GJ021B68JDT3` and `workos_updated_at=2026-04-24T10:53:32.86Z`.              |
| Organization WorkOS-link update | Mateffy         | Backfills `workos_id=org_01KPZHZ7658PG6DPJZZ0WCCMY9` and `workos_updated_at=2026-04-24T10:53:33.243Z`.             |
| Organization WorkOS-link update | maestro         | Backfills `workos_id=org_01KPZHZ7JRNSES5NY7TWZ17YZ4` and `workos_updated_at=2026-04-24T10:53:33.644Z`.             |
| Organization create             | Dead Rock Music | Creates local organization `4edf26ad-f5d0-4dd2-ba37-5aef2cd58f8e` for WorkOS org `org_01KPZHZNN30GS4YJEHV8AEAKH8`. |
| Organization create             | Grokbase        | Creates local organization `dff9c53b-6042-4d1c-972b-6728b5122503` for WorkOS org `org_01KPZHZPQB1Y1XF649AWT062W0`. |
| Organization create             | moisty          | Creates local organization `43067529-6441-4342-95ac-be05a77063d6` for WorkOS org `org_01KPZHZY6YVTZB1JMQXW5Y8GFG`. |

### Review Focus

- Review why 887 organizations exist locally without `workos_id`.
- Review the 111 organization creates.
- This batch has no user, membership, role, or assignment changes, so the practical risk is limited to organization identity-linking and organization creation.

## Organization Batch 8

### Overview

| Area                              | Count |
| --------------------------------- | ----: |
| WorkOS organizations scanned      | 1,000 |
| Organizations skipped             |     0 |
| Organization rows affected        |   999 |
| Organization rows to create       |    59 |
| Organization rows to update       |   940 |
| Organization rows already current |     1 |
| Organization roles to create      |     0 |
| Users to create                   |     1 |
| Memberships to create             |     1 |
| Role assignments to create        |     1 |
| Total planned changed records     | 1,002 |

No deletes are planned. This batch is mostly organization identity-linking, plus one user, one membership, and one role assignment create.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                                                         |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ----------------------------------------------------------------------------------------------------------------------------- |
| Organizations      |     59 |    940 |      0 |     1 |          0 | Updates backfill missing `workos_id` and `workos_updated_at`; creates add local org rows for WorkOS orgs not present locally. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                                                                                     |
| Users              |      1 |      0 |      0 |     0 |          0 | One missing WorkOS-linked user will be created.                                                                               |
| Memberships        |      1 |      0 |      0 |     0 |          0 | One membership will be created.                                                                                               |
| Role assignments   |      1 |      0 |      0 |     0 |          0 | One role assignment will be created.                                                                                          |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| orbidamarketing     | `e65a9698-0c83-484b-8a8a-2f9b9a47cd38` | Update org WorkOS ID and timestamp. |
| Gradingflow         | `28bffff4-55be-407c-9f11-100c14ef0bc4` | Update org WorkOS ID and timestamp. |
| BagelPay            | `4e18f3d3-8719-40f4-80a7-0febed3cf660` | Update org WorkOS ID and timestamp. |
| xyz                 | `88418da3-0a27-4c31-be34-52d5265bd7f2` | Update org WorkOS ID and timestamp. |
| asd-testing         | `f0c19b9a-a109-4ffe-957c-f4142276790e` | Update org WorkOS ID and timestamp. |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk     | Fields                                                 |
| ------------ | ------ | -----------: | -------- | ------------------------------------------------------ |
| Organization | Update |           96 | Identity | `workos_id`, `workos_updated_at`                       |
| Organization | Create |            4 | Identity | `id`, `name`, `slug`, `workos_id`, `workos_updated_at` |

### Notable Samples

| Change                          | Example         | Notes                                                                                                              |
| ------------------------------- | --------------- | ------------------------------------------------------------------------------------------------------------------ |
| Organization WorkOS-link update | orbidamarketing | Backfills `workos_id=org_01KPZJDF4NWG5QNV7VN73P6VFM` and `workos_updated_at=2026-04-24T11:01:20.138Z`.             |
| Organization WorkOS-link update | Gradingflow     | Backfills `workos_id=org_01KPZJDFG43CZ5B24BW6HPTNB6` and `workos_updated_at=2026-04-24T11:01:20.505Z`.             |
| Organization WorkOS-link update | BagelPay        | Backfills `workos_id=org_01KPZJDFVR50HSB1TR64BSNDCA` and `workos_updated_at=2026-04-24T11:01:20.878Z`.             |
| Organization create             | x               | Creates local organization `a4b8080b-42a5-46a3-ae55-1184d46c6b11` for WorkOS org `org_01KPZJE05G902B5VV91YG83Y1G`. |
| Organization create             | seia            | Creates local organization `59ab2eca-a9c2-4858-89db-238b996c741b` for WorkOS org `org_01KPZJE4DY5S65W5FZ22WM8F1J`. |
| Organization create             | seia            | Creates local organization `52cce0b0-5fc4-4c09-89d7-1870e99a1aa2` for WorkOS org `org_01KPZJE4SW3G9EZRJ40WAVD29S`. |

### Review Focus

- Review why 940 organizations exist locally without `workos_id`.
- Review the 59 organization creates. The script now prints a dedicated `organization_creates` section on rerun for the full list.
- Confirm the one user create, one membership create, and one assignment create are expected.
- This batch is mostly organization identity-linking; the RBAC impact is limited to one assignment create.

## Organization Batch 9

### Overview

| Area                              | Count |
| --------------------------------- | ----: |
| WorkOS organizations scanned      | 1,000 |
| Organizations skipped             |     0 |
| Organization rows affected        |   993 |
| Organization rows to create       |    58 |
| Organization rows to update       |   935 |
| Organization rows already current |     7 |
| Organization roles to create      |     0 |
| Users to update                   |     1 |
| Memberships to update             |     1 |
| Role assignments to create        |     1 |
| Total planned changed records     | 1,002 |

No deletes are planned. This batch is mostly organization identity-linking, plus one user update, one membership update, and one role assignment create.

Note: the row counters total 1,002 planned changes, while the sampled details block reports 996. Treat the row counters as authoritative.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                                                         |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ----------------------------------------------------------------------------------------------------------------------------- |
| Organizations      |     58 |    935 |      0 |     7 |          0 | Updates backfill missing `workos_id` and `workos_updated_at`; creates add local org rows for WorkOS orgs not present locally. |
| Organization roles |      0 |      0 |      0 |     0 |          0 | No role changes expected.                                                                                                     |
| Users              |      0 |      1 |      0 |     0 |          0 | One existing user will be updated.                                                                                            |
| Memberships        |      0 |      1 |      0 |     0 |          0 | One existing membership will be updated.                                                                                      |
| Role assignments   |      1 |      0 |      0 |     0 |          0 | One role assignment will be created.                                                                                          |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| Robomart            | `a7886b2c-8848-4986-b399-b907fe726f1e` | Update org WorkOS ID and timestamp. |
| fewline             | `45f13a4b-4862-467a-ae58-edf1979a8641` | Update org WorkOS ID and timestamp. |
| ny                  | `c26db38e-1338-44b0-973d-0e1f626b544e` | Update org WorkOS ID and timestamp. |
| Calvin              | `0e4b91f4-5961-4d5f-9fcf-58cc4964c2ec` | Create organization metadata.       |
| ErenTestCompany     | `2c68bdcb-304a-4a0f-b579-0408807e0f1d` | Update org WorkOS ID and timestamp. |

### Organization Creates

The script printed the full list of 58 planned organization creates for this batch. Examples include:

| WorkOS organization              | Gram organization                      | Name       |
| -------------------------------- | -------------------------------------- | ---------- |
| `org_01KPZJS411X0KW8WJESW2SJ07M` | `0e4b91f4-5961-4d5f-9fcf-58cc4964c2ec` | Calvin     |
| `org_01KPZJSA92JXQRC9PR7HWH7RPW` | `680ee007-4dd6-40b2-8582-6e725dba1999` | Jij        |
| `org_01KPZJSANGKPCRYGZCYZGA8S6N` | `f7fd1d77-18cc-4e0b-9145-60c6bf044213` | Jij        |
| `org_01KPZJSVY3Q6Q5WK034DADZ7FQ` | `f1fd0b29-e789-4657-a943-772c68b6ffc2` | EricTech   |
| `org_01KPZJTEHF25S5DTDGRAV5PP40` | `82626af2-7fac-46ec-9edb-cfd66b4247e2` | test       |
| `org_01KPZK4CJ67E3KBG6M32ZDNQR4` | `a41fca48-8006-4eec-a8f9-7e01b1579ef6` | Mistral AI |
| `org_01KPZK4FV9F808DGG3NTC6KRZ7` | `7156700b-14c3-47cd-ab3b-e7c542f70160` | Speakeasy. |
| `org_01KPZK4G76WJ23GXY2ZYC6XFSV` | `4e297057-3d29-4225-b8b9-0db65094888f` | Tango-Dazn |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk          | Fields                                                 |
| ------------ | ------ | -----------: | ------------- | ------------------------------------------------------ |
| Organization | Update |           95 | Identity      | `workos_id`, `workos_updated_at`                       |
| Organization | Create |            4 | Identity      | `id`, `name`, `slug`, `workos_id`, `workos_updated_at` |
| Organization | Update |            1 | Metadata only | `workos_updated_at`                                    |

### Review Focus

- Review why 935 organizations exist locally without `workos_id`.
- Review the 58 organization creates using the full `organization_creates` output.
- Confirm the one user update, one membership update, and one assignment create are expected.
- This batch is mostly organization identity-linking; the RBAC impact is limited to one assignment create.

## Organization Final Batch

### Overview

| Area                               | Count |
| ---------------------------------- | ----: |
| WorkOS organizations scanned       |   830 |
| Organizations skipped              |     0 |
| Organization rows affected         |   775 |
| Organization rows to create        |   168 |
| Organization rows to update        |   607 |
| Organization rows already current  |    55 |
| Organization roles to create       |    13 |
| Users to create                    |   122 |
| Users to update                    |    96 |
| User rows stale-skipped            |   163 |
| Memberships to create              |   125 |
| Memberships to update              |    93 |
| Membership rows stale-skipped      |   163 |
| Role assignments to create         |   217 |
| Role assignments already current   |     1 |
| Role assignment rows stale-skipped |   163 |
| Total planned changed records      | 1,441 |

No deletes are planned. This is the largest organization-create batch and also includes the final organization role creates.

### What Will Change

| Entity             | Create | Update | Delete | No-op | Stale skip | Notes                                                                                                      |
| ------------------ | -----: | -----: | -----: | ----: | ---------: | ---------------------------------------------------------------------------------------------------------- |
| Organizations      |    168 |    607 |      0 |    55 |          0 | Creates missing local organization rows and updates existing rows with WorkOS identity metadata.           |
| Organization roles |     13 |      0 |      0 |     0 |          0 | Creates thirteen WorkOS-backed organization roles.                                                         |
| Users              |    122 |     96 |      0 |     0 |        163 | Creates missing users and backfills WorkOS fields on existing users; stale skips are excluded from writes. |
| Memberships        |    125 |     93 |      0 |     0 |        163 | Creates or repairs WorkOS membership links; stale skips are excluded from writes.                          |
| Role assignments   |    217 |      0 |      0 |     1 |        163 | Creates role assignments for current memberships; stale skips are excluded from writes.                    |

### Sample Organizations

| WorkOS organization | Gram organization                      | Planned impact                      |
| ------------------- | -------------------------------------- | ----------------------------------- |
| Sabi                | `52fb5f8b-e13c-4317-a86e-ab717a3e6811` | Update org WorkOS ID and timestamp. |
| rugbysmiles         | `1df72a8d-4c60-4df1-b959-7ede4f75cc13` | Update org WorkOS ID and timestamp. |
| Popp AI             | `7d7be8f6-e6d1-4779-b98b-785296af417d` | Create organization metadata.       |
| popp                | `e95e42ec-39c2-45f7-852a-eab681b6ed4b` | Create organization metadata.       |
| Popp AI             | `c1865861-4fac-45a2-ac59-b0dad9414184` | Create organization metadata.       |

### Organization Creates

The script printed the full list of 168 planned organization creates for this batch. Examples include:

| WorkOS organization              | Gram organization                      | Name         |
| -------------------------------- | -------------------------------------- | ------------ |
| `org_01KPZK4JQ0SGQPWJBHEZ1NB1TJ` | `7d7be8f6-e6d1-4779-b98b-785296af417d` | Popp AI      |
| `org_01KPZK4K2SA1S6BFPB1DZJCTNG` | `e95e42ec-39c2-45f7-852a-eab681b6ed4b` | popp         |
| `org_01KPZK4KHP0X0YW4XTQNQ9DKQW` | `c1865861-4fac-45a2-ac59-b0dad9414184` | Popp AI      |
| `org_01KPZK4KWT9NJ17AZCSVK8NCKQ` | `8534bc7c-f6b1-478e-b151-fc7734d59ce8` | Popp AI      |
| `org_01KPZK4CJ67E3KBG6M32ZDNQR4` | `a41fca48-8006-4eec-a8f9-7e01b1579ef6` | Mistral AI   |
| `org_01KPZK4FV9F808DGG3NTC6KRZ7` | `7156700b-14c3-47cd-ab3b-e7c542f70160` | Speakeasy.   |
| `org_01KQXQBZ2ZNFQN860QNNWERNNW` | `1eefa2a2-431c-46bd-a7f8-59ff5a72633b` | ConductorOne |
| `org_01KR1F5VT23XP2DHZ5611P67BP` | `c9e0e5a2-b13f-4260-ac2a-bce32bc235b0` | Benchling    |
| `org_01KRK5W5GDX1D209EMP8KK5GMB` | `a72784d5-68ad-4f2c-a856-2c78295ffcec` | Glean        |
| `org_01KRMWJ0X2B0C0H6JKHCB9NSTW` | `da0d1b16-5832-4497-8eef-78aebd2a221c` | GrowthBook   |

### Planned Change Groups

The detailed change summary is sampled. The row counts above are authoritative.

| Entity       | Action | Sample count | Risk     | Fields                                                 |
| ------------ | ------ | -----------: | -------- | ------------------------------------------------------ |
| Organization | Update |           83 | Identity | `workos_id`, `workos_updated_at`                       |
| Organization | Create |           17 | Identity | `id`, `name`, `slug`, `workos_id`, `workos_updated_at` |

### Review Focus

- Review the 168 organization creates; this is the highest create count of any batch.
- Review the 13 organization role creates before running writes.
- Confirm the 217 role assignment creates are expected now that global roles exist locally.
- Review the 163 stale skips across users, memberships, and assignments; these rows are intentionally not written.
