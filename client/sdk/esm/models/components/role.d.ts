import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { RoleGrant } from "./rolegrant.js";
export type Role = {
  createdAt: Date;
  /**
   * Human-readable description.
   */
  description: string;
  /**
   * Scope grants assigned to this role.
   */
  grants: Array<RoleGrant>;
  /**
   * Unique role identifier.
   */
  id: string;
  /**
   * Whether this is a built-in system role that cannot be deleted.
   */
  isSystem: boolean;
  /**
   * Number of members assigned to this role.
   */
  memberCount: number;
  /**
   * Display name of the role.
   */
  name: string;
  /**
   * Canonical principal URN for this role.
   */
  principalUrn: string;
  /**
   * Stable WorkOS role slug.
   */
  slug: string;
  updatedAt: Date;
};
/** @internal */
export declare const Role$inboundSchema: z.ZodMiniType<Role, unknown>;
export declare function roleFromJSON(
  jsonString: string,
): SafeParseResult<Role, SDKValidationError>;
//# sourceMappingURL=role.d.ts.map
