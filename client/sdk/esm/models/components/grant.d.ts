import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A permission record giving a user or role the ability to perform an action on a resource.
 */
export type Grant = {
  /**
   * When this permission was granted.
   */
  createdAt: Date;
  /**
   * Unique identifier of this permission.
   */
  id: string;
  /**
   * The organization this permission belongs to.
   */
  organizationId: string;
  /**
   * Whether the principal is a user or a role.
   */
  principalType: string;
  /**
   * The user or role that holds this permission (e.g. "user:user_abc", "role:admin").
   */
  principalUrn: string;
  /**
   * The resource this permission applies to. "*" means all resources.
   */
  resource: string;
  /**
   * The action this permission allows (e.g. "build:read", "mcp:connect").
   */
  scope: string;
  /**
   * When this permission was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const Grant$inboundSchema: z.ZodMiniType<Grant, unknown>;
export declare function grantFromJSON(
  jsonString: string,
): SafeParseResult<Grant, SDKValidationError>;
//# sourceMappingURL=grant.d.ts.map
