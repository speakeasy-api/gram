import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Role } from "./role.js";
export type ListRolesResult = {
  /**
   * The roles in your organization.
   */
  roles: Array<Role>;
};
/** @internal */
export declare const ListRolesResult$inboundSchema: z.ZodMiniType<
  ListRolesResult,
  unknown
>;
export declare function listRolesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListRolesResult, SDKValidationError>;
//# sourceMappingURL=listrolesresult.d.ts.map
