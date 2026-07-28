import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ListRoleGrant } from "./listrolegrant.js";
export type ListUserGrantsResult = {
  /**
   * The user's effective grants in this organization.
   */
  grants: Array<ListRoleGrant>;
};
/** @internal */
export declare const ListUserGrantsResult$inboundSchema: z.ZodMiniType<
  ListUserGrantsResult,
  unknown
>;
export declare function listUserGrantsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListUserGrantsResult, SDKValidationError>;
//# sourceMappingURL=listusergrantsresult.d.ts.map
