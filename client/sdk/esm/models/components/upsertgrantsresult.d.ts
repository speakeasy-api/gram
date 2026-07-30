import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Grant } from "./grant.js";
export type UpsertGrantsResult = {
  /**
   * The permissions that were created or already existed.
   */
  grants: Array<Grant>;
};
/** @internal */
export declare const UpsertGrantsResult$inboundSchema: z.ZodMiniType<
  UpsertGrantsResult,
  unknown
>;
export declare function upsertGrantsResultFromJSON(
  jsonString: string,
): SafeParseResult<UpsertGrantsResult, SDKValidationError>;
//# sourceMappingURL=upsertgrantsresult.d.ts.map
