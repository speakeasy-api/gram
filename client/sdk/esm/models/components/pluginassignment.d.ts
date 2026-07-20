import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type PluginAssignment = {
  createdAt: Date;
  /**
   * Unique assignment identifier.
   */
  id: string;
  /**
   * Principal URN (e.g. role:engineering, user:id, or *).
   */
  principalUrn: string;
};
/** @internal */
export declare const PluginAssignment$inboundSchema: z.ZodMiniType<
  PluginAssignment,
  unknown
>;
export declare function pluginAssignmentFromJSON(
  jsonString: string,
): SafeParseResult<PluginAssignment, SDKValidationError>;
//# sourceMappingURL=pluginassignment.d.ts.map
