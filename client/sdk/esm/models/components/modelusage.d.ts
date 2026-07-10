import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Model usage statistics
 */
export type ModelUsage = {
  /**
   * Number of times used
   */
  count: number;
  /**
   * Model name
   */
  name: string;
};
/** @internal */
export declare const ModelUsage$inboundSchema: z.ZodMiniType<
  ModelUsage,
  unknown
>;
export declare function modelUsageFromJSON(
  jsonString: string,
): SafeParseResult<ModelUsage, SDKValidationError>;
//# sourceMappingURL=modelusage.d.ts.map
