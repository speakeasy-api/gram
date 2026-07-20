import * as z from "zod/v4-mini";
import { RoleGrant, RoleGrant$Outbound } from "./rolegrant.js";
export type UpdateRoleForm = {
  /**
   * Scope grants to add.
   */
  addGrants?: Array<RoleGrant> | undefined;
  /**
   * Updated description.
   */
  description?: string | undefined;
  /**
   * The ID of the role to update.
   */
  id: string;
  /**
   * Optional member IDs to additionally assign to this role. Existing assignments are preserved.
   */
  memberIds?: Array<string> | undefined;
  /**
   * Updated display name.
   */
  name?: string | undefined;
  /**
   * Scope grants to remove.
   */
  removeGrants?: Array<RoleGrant> | undefined;
};
/** @internal */
export type UpdateRoleForm$Outbound = {
  add_grants?: Array<RoleGrant$Outbound> | undefined;
  description?: string | undefined;
  id: string;
  member_ids?: Array<string> | undefined;
  name?: string | undefined;
  remove_grants?: Array<RoleGrant$Outbound> | undefined;
};
/** @internal */
export declare const UpdateRoleForm$outboundSchema: z.ZodMiniType<
  UpdateRoleForm$Outbound,
  UpdateRoleForm
>;
export declare function updateRoleFormToJSON(
  updateRoleForm: UpdateRoleForm,
): string;
//# sourceMappingURL=updateroleform.d.ts.map
