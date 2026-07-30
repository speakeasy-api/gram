import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Grant } from "./grant.js";
export type ListGrantsResult = {
  /**
   * The permissions in your organization.
   */
  grants: Array<Grant>;
};
/** @internal */
export declare const ListGrantsResult$inboundSchema: z.ZodMiniType<
  ListGrantsResult,
  unknown
>;
export declare function listGrantsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListGrantsResult, SDKValidationError>;
//# sourceMappingURL=listgrantsresult.d.ts.map
