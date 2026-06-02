# Role-Based Access Control (RBAC)

Role-Based Access Control (RBAC) lets you manage which members of your organization can view, modify, and use resources in the Speakeasy AI Platform. Use it to give every teammate the right level of access — and nothing more.

This guide explains how RBAC works in the Speakeasy AI Platform, how to manage roles and members from the dashboard, and how RBAC interacts with directory sync.

> Screenshots referenced in this guide live in [`docs/images/rbac/`](images/rbac/). Capture them from a dashboard with RBAC enabled and at least one custom role configured.

---

## What is RBAC?

RBAC is the access-control model the Speakeasy AI Platform uses to decide whether a given user is allowed to perform a given action on a given resource. It is built around four ideas:

- **Permissions (scopes)** — the operations the platform can authorize, such as reading projects or connecting to an MCP server.
- **Roles** — named bundles of permissions you can assign to users.
- **Members** — the users in your organization who are assigned one or more roles.
- **Resources** — the projects, MCP servers, and environments that the permissions apply to.

When a user makes a request, the platform looks at the roles assigned to them, gathers the permissions those roles include, and checks whether any of those permissions allow the requested action. If none do, the request is rejected.

> **Screenshot:** Access page overview — `Settings → Access → Roles & Permissions`, showing the **Roles**, **Members**, and **Authorization Challenges** tabs. Save as `docs/images/rbac/access-overview.png`.

---

## Roles

A role is a named collection of permissions you can assign to one or more members. The Speakeasy AI Platform ships with two built-in roles and lets you define your own.

### Built-in roles

| Role     | Description                                                                                                                                                                              |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `admin`  | Full access to the organization, all projects, MCP servers, and environments. Administrators can manage other members, roles, and settings.                                              |
| `member` | Read-only access to organization metadata, projects, MCP server configuration, and environments — plus the ability to connect to MCP servers. Members cannot create or modify resources. |

Built-in roles cannot be renamed, deleted, or have their permission set changed. You can always assign or unassign them on the **Members** tab.

### Custom roles

You can create additional roles to fit your team's structure — for example, a `mcp-developer` role that can write MCP servers but not change org settings, or a `read-only-auditor` role that has read scopes only.

To create a custom role:

1. Open the dashboard and go to **Settings → Access → Roles & Permissions**.
2. Select the **Roles** tab.
3. Click **Create role**.
4. Give the role a name and pick the permissions it should include from the scope picker.
5. Save.

> **Screenshot:** Create-role dialog, showing the name field and the scope picker open. Save as `docs/images/rbac/create-role.png`.

You can edit a custom role at any time to add or remove permissions; changes take effect on the next request. Deleting a custom role unassigns it from every member.

### Multiple roles per member

A member can hold more than one role at the same time. Their effective permissions are the **union** of every scope on every role assigned to them — there are no deny rules. If any role assigned to the user grants the requested permission, the request is allowed. If no role grants it, the request is denied. (See [Evaluation](#evaluation) for the full rules.)

This makes it easy to compose access: a teammate might be a `member` of the org and additionally hold a `mcp-developer` role on top, picking up MCP write permissions without losing the default member permissions.

---

## Permissions (Scopes)

A **scope** is a single named permission such as "view projects" or "connect to MCP servers." Every scope has the form `<resource>:<verb>`, and the platform exposes the full catalogue in the **Roles & Permissions** UI so you can see exactly what each role grants.

### Available scopes

| Scope               | Resource type | What it allows                                                     |
| ------------------- | ------------- | ------------------------------------------------------------------ |
| `org:read`          | Organization  | View organization metadata and the list of members.                |
| `org:admin`         | Organization  | Manage organization access, settings, and member role assignments. |
| `project:read`      | Project       | View projects and project-level resources.                         |
| `project:write`     | Project       | Create and modify projects and project-level resources.            |
| `mcp:read`          | MCP server    | View MCP servers and their configuration.                          |
| `mcp:write`         | MCP server    | Create and modify MCP servers and their configuration.             |
| `mcp:connect`       | MCP server    | Connect to and use MCP servers (e.g. from a client).               |
| `environment:read`  | Environment   | View environments and their entries inside a project.              |
| `environment:write` | Environment   | Add, edit, clone, and remove environments inside a project.        |

### Scope hierarchy (write implies read)

Within a resource family, higher-privilege scopes automatically satisfy lower-privilege ones. You don't need to grant both `:write` and `:read` — granting `:write` already covers `:read`.

- `project:write` ⊇ `project:read`
- `mcp:write` ⊇ `mcp:read`
- `mcp:connect` is satisfied by `mcp:read` or `mcp:write` (it is the broadest, easiest gate to clear — anyone with read or write also has connect)
- `environment:write` ⊇ `environment:read`
- `org:admin` ⊇ `org:read`

Scope expansion does **not** cross resource boundaries: `project:read` does not grant any `environment:*` permission, because environment values may contain secrets that a project viewer should not see.

> **Screenshot:** Scope picker popover open on the create- or edit-role dialog, showing scopes grouped by resource type. Save as `docs/images/rbac/scope-picker.png`.

---

## Evaluation

When a user makes a request, the platform evaluates access by combining every role assigned to that user and checking whether the union of their scopes satisfies the request.

### Allow-only model

The Speakeasy AI Platform's RBAC is **allow-only**. Roles can only grant access; there is no concept of an explicit deny rule that overrides an allow. To restrict a user, remove the role that grants the unwanted access — don't try to layer a "deny" role on top.

### Multiple roles

When a user holds multiple roles, the platform takes the union of every scope across those roles. If any role grants the required permission for the target resource, the request is allowed.

Example: a user with both `member` (which includes `mcp:read`) and a custom `mcp-developer` role (which adds `mcp:write`) can both view and modify MCP servers. They retain everything the `member` role gives them.

### Resource scoping

A scope grant always names the resource it applies to. A grant can be:

- **Resource-specific** — e.g. write access only to one project.
- **Wildcard** — applies to every resource of that type in the organization (the default for roles created from the dashboard).

When checking access, the platform only allows the request if at least one of the user's grants covers both the requested **scope** and the requested **resource ID**.

### Scope expansion

Higher-privilege scopes automatically satisfy lower ones within the same resource family. For example, a user with `mcp:write` on a server can also read it and connect to it without needing those scopes assigned separately. See [Scope hierarchy](#scope-hierarchy-write-implies-read) for the full mapping.

### What if access is denied?

When a request is denied, the dashboard renders an "Unauthorized" message or hides/disables the affected control. The **Authorization Challenges** tab on the Access page surfaces recent denied requests so admins can see what their teammates were trying to do and adjust roles accordingly.

> **Screenshot:** Authorization Challenges tab showing a list of recent denied requests with the requesting user, scope, and resource. Save as `docs/images/rbac/authz-challenges.png`.

---

## Managing members

Use the **Members** tab on the Access page to see every user in your organization and the roles they currently hold.

To change a member's role:

1. Go to **Settings → Access → Members**.
2. Find the member in the table.
3. Click the role column to open the role picker.
4. Select one or more roles, then save.

> **Screenshot:** Members tab with the role picker popover open on one row. Save as `docs/images/rbac/members-tab.png`.

> **Note:** When directory sync is enabled, role assignments are managed by your identity provider — see [RBAC and directory sync](#rbac-and-directory-sync) below. The role picker on the Members tab is read-only in that mode.

---

## RBAC and directory sync

If your organization uses an identity provider (such as Okta, Microsoft Entra ID, or Google Workspace) and has directory sync configured, the Speakeasy AI Platform can mirror group membership from your IdP and use directory groups to assign roles automatically.

### How it works

1. Your IdP groups are synchronized into the platform as a snapshot.
2. Each directory group is mapped to one platform role.
3. When a user is in a directory group, they are assigned the corresponding role on the platform.
4. When a user is added to or removed from a group in the IdP, the change is reflected in their role assignments shortly after.

### Important rules under directory sync

- **Memberships are managed by your IdP, not by the dashboard.** With directory sync on, the **Members** tab is read-only — you cannot edit role assignments from the dashboard. To grant or revoke a role, update the user's group membership in your identity provider.
- **Roles must exist on the platform before you can map a group to them.** Create the custom roles you want to use first, then configure the group-to-role mapping in the directory sync settings.
- **A user in multiple groups gets multiple roles.** If a directory user belongs to two groups that both map to roles, they hold both roles, and their effective permissions are the union (see [Multiple roles](#multiple-roles) above).
- **Removing a user from a group removes the corresponding role.** If they are not in any mapped group, they lose all role-based access. Make sure every user belongs to at least one mapped group (commonly an "all employees" group mapped to the `member` role).

### Enabling directory sync

Directory sync is configured per-organization and requires admin permissions on both the Speakeasy AI Platform and your identity provider.

1. Go to **Settings → Access → Directory sync** in the dashboard.
2. Follow the setup wizard — the platform will provide the credentials and endpoint URLs you need to register it as an application in your IdP.
3. Once the connection is verified, map each directory group to a role.
4. Save the mapping. Existing group members will be assigned the corresponding roles on the next sync.

> **Screenshot:** Directory sync configuration page, group-to-role mapping table. Save as `docs/images/rbac/directory-sync-mapping.png`.

If you need help connecting the Speakeasy AI Platform to your identity provider, reach out to support — we can walk you through the IdP-specific setup steps.

---

## Frequently asked questions

**Can I deny a permission to a specific user who has it via a role?**
No — RBAC on the Speakeasy AI Platform is allow-only. If a user shouldn't have a permission, remove the role that grants it (or use a different role that doesn't include that scope).

**What permission do I need to manage roles and members?**
You need `org:admin`. The built-in `admin` role includes this; the `member` role does not.

**Can a custom role include `org:admin`?**
Yes. Any combination of scopes is allowed in a custom role. Be conservative — granting `org:admin` lets that role manage all access, including creating new admins.

**Do read scopes give access to environment values (secrets)?**
Only `environment:read` (or `environment:write`) does. Holding `project:read` is not enough — environment access is deliberately scoped separately so a project viewer cannot read secrets stored in environments.
