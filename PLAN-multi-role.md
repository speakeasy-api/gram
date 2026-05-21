# Multi-Role Support Plan

## Goal

Enable WorkOS directory sync by supporting multiple roles per user. Previously single-role only.

## Decisions

- Full replacement semantics for role updates (not additive)
- Show all roles (no "primary" concept)
- No external API consumers → no backward compat needed
- Single role for invite (keep simple)
- Admin protection: cannot remove last admin role assignment

## Status

### Backend — DONE

| File                                                                    | Change                                                                                                                                                              | Status |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ |
| `server/internal/thirdparty/workos/user.go`                             | Add `RoleSlugs []string` to `Member`, add `UpdateMemberRoles` method                                                                                                | Done   |
| `server/internal/thirdparty/workos/client.go`                           | `convertMember` populates `RoleSlugs` from `m.Roles` with fallback                                                                                                  | Done   |
| `server/internal/thirdparty/workos/stub.go`                             | Stub `UpdateMemberRoles`, update all member constructors                                                                                                            | Done   |
| `server/design/access/design.go`                                        | `role_id String` → `role_ids ArrayOf(String)` on `AccessMember` and `UpdateMemberRoleForm`                                                                          | Done   |
| `server/gen/**`                                                         | Regenerated via `mise run gen:goa-server`                                                                                                                           | Done   |
| `server/internal/access/impl.go`                                        | `ListMembers` builds `roleIDs` array; `UpdateMemberRole` validates array, resolves all IDs→slugs, admin protection via `slices.Contains`, calls `UpdateMemberRoles` | Done   |
| `server/internal/authz/engine.go`                                       | `resolveRoleSlug` → `resolveRoleSlugs` returning `[]string`; `LoadGrantsForContext` adds ALL role principals                                                        | Done   |
| `server/internal/background/activities/backfill_workos_organization.go` | Use `member.RoleSlugs` with fallback                                                                                                                                | Done   |
| `server/internal/access/*_test.go`                                      | All tests updated for multi-role payloads/assertions, admin protection coverage                                                                                     | Done   |
| `server/internal/authz/engine_test.go`                                  | Updated for `resolveRoleSlugs`                                                                                                                                      | Done   |

### SDK — DONE

| File                          | Change                                                                                                          | Status |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------- | ------ |
| `client/sdk/`                 | Regenerated via `mise run gen:sdk` — `AccessMember.roleIds: string[]`, `UpdateMemberRoleForm.roleIds: string[]` | Done   |
| `.speakeasy/out.openapi.yaml` | Updated                                                                                                         | Done   |

### Frontend — DONE

| File                                                            | Change                                                                                                   | Status |
| --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | ------ |
| `client/dashboard/src/pages/access/ChangeRoleDialog.tsx`        | Rewritten: single-select → multi-select checkbox list                                                    | Done   |
| `client/dashboard/src/pages/team/Team.tsx`                      | `roleByUserId` → `roleIdsByUserId` (array), multiple role badges, admin count uses `.roleIds.includes()` | Done   |
| `client/dashboard/src/pages/access/RolesTab.tsx`                | `.roleId === deletingRole.id` → `.roleIds.includes(deletingRole.id)`                                     | Done   |
| `client/dashboard/src/pages/access/CreateRoleDialog.tsx`        | All 9 `.roleId` refs → `.roleIds` array methods                                                          | Done   |
| `client/dashboard/src/pages/mcp/MCPTeamAccessTab.tsx`           | `MemberAccess.role` → `roles: Role[]`, aggregate scopes across roles, show all role names                | Done   |
| `client/dashboard/src/components/observe/InsightsEmployees.tsx` | Map over `member.roleIds` array, join resolved names                                                     | Done   |

### Verification

| Check                                        | Status                                                  |
| -------------------------------------------- | ------------------------------------------------------- |
| `go build ./server/...`                      | Pass                                                    |
| `mise lint:server`                           | Pass                                                    |
| `npx tsc --noEmit` (dashboard)               | Pass                                                    |
| `mise run test:server ./internal/access/...` | Pending (infra timeout on last run, code fixes applied) |

### Remaining

- [ ] Run server tests to confirm all pass
- [ ] Add changeset file (`.changeset/<slug>.md`)
- [ ] Commit and push
