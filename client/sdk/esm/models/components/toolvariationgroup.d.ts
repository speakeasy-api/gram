import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ToolVariationGroup = {
  /**
   * The creation date of the tool variation group
   */
  createdAt: string;
  /**
   * The description of the tool variation group
   */
  description?: string | undefined;
  /**
   * The ID of the tool variation group
   */
  id: string;
  /**
   * The name of the tool variation group
   */
  name: string;
  /**
   * The last update date of the tool variation group
   */
  updatedAt: string;
};
/** @internal */
export declare const ToolVariationGroup$inboundSchema: z.ZodMiniType<
  ToolVariationGroup,
  unknown
>;
export declare function toolVariationGroupFromJSON(
  jsonString: string,
): SafeParseResult<ToolVariationGroup, SDKValidationError>;
//# sourceMappingURL=toolvariationgroup.d.ts.map
