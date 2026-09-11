import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { useRoles } from "@gram/client/react-query/roles.js";
import { type JSX } from "react";
import { useNavigate, useParams } from "react-router";
import { CreateRoleDialog } from "./CreateRoleDialog";

/**
 * Create or edit one role on its own page. The same editor still opens as a
 * sheet from other surfaces; only the chrome differs, so the two cannot drift.
 */
export function RoleEditorPage(): JSX.Element {
  const { roleId } = useParams();
  const navigate = useNavigate();
  const orgRoutes = useOrgRoutes();
  const { data, isLoading } = useRoles();

  const role = roleId
    ? (data?.roles ?? []).find((candidate) => candidate.id === roleId)
    : undefined;
  const backToRoles = () => void navigate(orgRoutes.access.roles.href());

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
                  onOpenChange={(open) => {
                    if (!open) backToRoles();
                  }}
                  onRoleCreated={backToRoles}
                />
              )}
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
      </Page.Body>
    </Page>
  );
}
