import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { useCreateRoleMutation } from "@gram/client/react-query/createRole.js";
import { invalidateAllGrants } from "@gram/client/react-query/grants.js";
import {
  invalidateAllMembers,
  useMembers,
} from "@gram/client/react-query/members.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import { useUpdateMemberRolesMutation } from "@gram/client/react-query/updateMemberRoles.js";
import { useOrganization, useUser } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { handleAPIError } from "@/lib/errors";

/**
 * The custom role setup creates so an admin can watch a conversation land.
 * Not a system role: it is created by admin action when a card needs it, and
 * from then on it is an ordinary custom role — editable, deletable, and
 * reusable. Only the membership is handed back afterwards, never the role.
 */
export const SESSION_AUDITOR_ROLE_NAME = "Session Auditor";

/**
 * What the server's `slugify` makes of the name above. Roles are unique per
 * organization on their slug, so this is what finds an existing one — the
 * display name could have been edited since.
 */
export const SESSION_AUDITOR_ROLE_SLUG = "org-session-auditor";

export const SESSION_AUDITOR_ROLE_DESCRIPTION =
  "Reads other members' agent sessions. Created during setup so an admin can confirm traffic arrives; remove yourself once it has.";

export interface SessionAuditAccess {
  /**
   * Whether this organization assigns the role in Speakeasy at all. False
   * under directory sync, where the identity provider owns role assignment,
   * and false until the caller's own membership record has loaded — both
   * assignment paths address the caller by user id.
   */
  available: boolean;
  /** The caller is currently a member of the Session Auditor role. */
  holdsRole: boolean;
  /** The caller can already read other members' sessions. */
  canReadSessions: boolean;
  /** Role assignment belongs to the identity provider, not to Speakeasy. */
  scimManaged: boolean;
  /** The Session Auditor role already exists in this organization. */
  roleExists: boolean;
  /** A create or assignment write is in flight. */
  isPending: boolean;
  /** Create the role if needed and add the caller to it. */
  grant: () => void;
  /** Create the role without assigning anyone, for directory-synced orgs. */
  ensureRole: () => void;
  /** Take the caller back out of the role, leaving the role in place. */
  revoke: () => void;
}

/** A role of this name already exists — someone else's setup, or a second card. */
function isConflict(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "statusCode" in error &&
    (error as { statusCode: unknown }).statusCode === 409
  );
}

/**
 * The Session Auditor role, from the point of view of the admin running
 * setup: whether they can read other members' sessions, and the two writes
 * that change that. Both go through the ordinary role endpoints, so every
 * change lands in the audit log the same way an Access page edit would.
 */
export function useSessionAuditAccess(): SessionAuditAccess {
  const queryClient = useQueryClient();
  const user = useUser();
  const organization = useOrganization();
  const { hasScope } = useRBAC();

  const rolesQuery = useRoles(undefined, undefined, { throwOnError: false });
  const membersQuery = useMembers(undefined, undefined, {
    throwOnError: false,
  });

  const auditorRole = rolesQuery.data?.roles.find(
    (role) => role.slug === SESSION_AUDITOR_ROLE_SLUG,
  );
  const me = membersQuery.data?.members.find((member) => member.id === user.id);

  const scimManaged = organization.scimEnabled === true;
  const holdsRole = Boolean(
    auditorRole && me?.roleIds.includes(auditorRole.id),
  );
  const canReadSessions = hasScope("chat:read");

  const createRole = useCreateRoleMutation();
  const updateMemberRoles = useUpdateMemberRolesMutation();

  // Grants are read per request rather than cached, so dropping all three
  // caches is enough for the next poll to run under the new role.
  const refresh = useCallback(
    () =>
      Promise.all([
        invalidateAllGrants(queryClient),
        invalidateAllRoles(queryClient),
        invalidateAllMembers(queryClient),
      ]),
    [queryClient],
  );

  const createAuditorRole = useCallback(
    (memberIds: string[] | undefined) =>
      createRole.mutateAsync({
        request: {
          createRoleForm: {
            name: SESSION_AUDITOR_ROLE_NAME,
            description: SESSION_AUDITOR_ROLE_DESCRIPTION,
            // `selectors: undefined` is the unrestricted grant, the same
            // shape the Access page's role form sends for a rule with no
            // resource narrowing.
            grants: [{ scope: Scope.ChatRead, selectors: undefined }],
            memberIds,
          },
        },
      }),
    [createRole],
  );

  const grant = useCallback(() => {
    void (async () => {
      if (!me) return;
      try {
        let roleId = auditorRole?.id;
        if (!roleId) {
          try {
            await createAuditorRole([user.id]);
            await refresh();
            return;
          } catch (error) {
            // Another admin created the role between our last roles read and
            // this click. Find the one that won and assign against it.
            if (!isConflict(error)) throw error;
            const refetched = await rolesQuery.refetch();
            roleId = refetched.data?.roles.find(
              (role) => role.slug === SESSION_AUDITOR_ROLE_SLUG,
            )?.id;
            if (!roleId) throw error;
          }
        }
        // updateMemberRoles replaces the whole list, so the caller's existing
        // roles — Admin among them — have to be sent back with it.
        await updateMemberRoles.mutateAsync({
          request: {
            updateMemberRolesForm: {
              userId: user.id,
              roleIds: [...new Set([...me.roleIds, roleId])],
            },
          },
        });
        await refresh();
      } catch (error) {
        handleAPIError(error, "Couldn't add you as a Session Auditor");
      }
    })();
  }, [
    auditorRole,
    createAuditorRole,
    me,
    refresh,
    rolesQuery,
    updateMemberRoles,
    user.id,
  ]);

  const ensureRole = useCallback(() => {
    void (async () => {
      if (auditorRole) return;
      try {
        await createAuditorRole(undefined);
        await refresh();
      } catch (error) {
        if (isConflict(error)) {
          await refresh();
          return;
        }
        handleAPIError(error, "Couldn't create the Session Auditor role");
      }
    })();
  }, [auditorRole, createAuditorRole, refresh]);

  const revoke = useCallback(() => {
    void (async () => {
      if (!me || !auditorRole) return;
      try {
        await updateMemberRoles.mutateAsync({
          request: {
            updateMemberRolesForm: {
              userId: user.id,
              roleIds: me.roleIds.filter((id) => id !== auditorRole.id),
            },
          },
        });
        await refresh();
      } catch (error) {
        handleAPIError(error, "Couldn't remove your Session Auditor access");
      }
    })();
  }, [auditorRole, me, refresh, updateMemberRoles, user.id]);

  return {
    available: Boolean(me) && !scimManaged,
    holdsRole,
    canReadSessions,
    scimManaged,
    roleExists: Boolean(auditorRole),
    isPending: createRole.isPending || updateMemberRoles.isPending,
    grant,
    ensureRole,
    revoke,
  };
}
