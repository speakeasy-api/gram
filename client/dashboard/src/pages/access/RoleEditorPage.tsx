import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { Role } from "@gram/client/models/components/role.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { type JSX, useRef } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import {
  DIRECTORY_MAPPING_FLOW,
  directoryMappingReturnParams,
  suggestedRoleName,
} from "../org/identity-provider/directoryMappingFlow";
import { CreateRoleDialog } from "./CreateRoleDialog";

/**
 * Create or edit one role on its own page. The same editor still opens as a
 * sheet from other surfaces; only the chrome differs, so the two cannot drift.
 *
 * Opened from a directory role mapping, it hides member and agent assignment
 * (the mapping grants the role) and returns to the mapping with the new role.
 */
export function RoleEditorPage(): JSX.Element {
  const { roleId } = useParams();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const orgRoutes = useOrgRoutes();
  const { data, isLoading } = useRoles();

  const fromMapping = !roleId && params.get("from") === DIRECTORY_MAPPING_FLOW;
  // Creating a role also closes the editor; the close must not undo the
  // navigation that carries the new role back to the mapping.
  const created = useRef(false);

  const role = roleId
    ? (data?.roles ?? []).find((candidate) => candidate.id === roleId)
    : undefined;
  const leave = () => {
    if (fromMapping) void navigate(orgRoutes.identity.href());
    else void navigate(orgRoutes.access.roles.href());
  };
  const onRoleCreated = (createdRole: Role) => {
    created.current = true;
    if (!fromMapping) {
      leave();
      return;
    }
    const back = directoryMappingReturnParams(params, createdRole.principalUrn);
    void navigate(`${orgRoutes.identity.href()}?${back.toString()}`);
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            {/* Eyebrow says what you are doing, title says to what — the
                same shape every other detail page uses. */}
            <Page.Section.Title area={roleId ? "Edit role" : "Roles"}>
              {roleId ? (role?.name ?? "Role") : "Create role"}
            </Page.Section.Title>
            <Page.Section.Description>
              Roles carry permissions, enabling access to different features in
              the product. Some permissions can be narrowed to include or
              exclude certain resources.
            </Page.Section.Description>
            <Page.Section.Body>
              {isLoading && roleId ? (
                <SkeletonTable />
              ) : roleId && !role ? (
                <Text muted small>
                  This role no longer exists.
                </Text>
              ) : (
                <CreateRoleDialog
                  open
                  presentation="page"
                  editingRole={role ?? null}
                  hideAssignments={fromMapping}
                  defaultName={fromMapping ? suggestedRoleName(params) : ""}
                  onOpenChange={(open) => {
                    if (!open && !created.current) leave();
                  }}
                  onRoleCreated={onRoleCreated}
                />
              )}
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
