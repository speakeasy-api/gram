import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A link between a toolset and an environment
 */
export type ToolsetEnvironmentLink = {
  /**
   * The ID of the environment
   */
  environmentId: string;
  /**
   * The ID of the toolset environment link
   */
  id: string;
  /**
   * The ID of the toolset
   */
  toolsetId: string;
};
/** @internal */
export declare const ToolsetEnvironmentLink$inboundSchema: z.ZodMiniType<
  ToolsetEnvironmentLink,
  unknown
>;
export declare function toolsetEnvironmentLinkFromJSON(
  jsonString: string,
): SafeParseResult<ToolsetEnvironmentLink, SDKValidationError>;
//# sourceMappingURL=toolsetenvironmentlink.d.ts.map
