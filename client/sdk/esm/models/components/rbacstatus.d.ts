import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RBACStatus = {
  /**
   * Whether RBAC enforcement is currently enabled for this organization.
   */
  rbacEnabled: boolean;
};
/** @internal */
export declare const RBACStatus$inboundSchema: z.ZodMiniType<
  RBACStatus,
  unknown
>;
export declare function rbacStatusFromJSON(
  jsonString: string,
): SafeParseResult<RBACStatus, SDKValidationError>;
//# sourceMappingURL=rbacstatus.d.ts.map
