import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Hook source usage statistics
 */
export type HookSourceUsage = {
  /**
   * Total hook events for this source
   */
  eventCount: number;
  /**
   * Hook source (from attributes.gram.hook.source)
   */
  source: string;
};
/** @internal */
export declare const HookSourceUsage$inboundSchema: z.ZodMiniType<
  HookSourceUsage,
  unknown
>;
export declare function hookSourceUsageFromJSON(
  jsonString: string,
): SafeParseResult<HookSourceUsage, SDKValidationError>;
//# sourceMappingURL=hooksourceusage.d.ts.map
