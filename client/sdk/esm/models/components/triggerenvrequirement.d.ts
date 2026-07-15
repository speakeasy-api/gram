import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type TriggerEnvRequirement = {
  /**
   * Description of the variable.
   */
  description?: string | undefined;
  /**
   * The environment variable name.
   */
  name: string;
  /**
   * Whether the variable is required.
   */
  required: boolean;
};
/** @internal */
export declare const TriggerEnvRequirement$inboundSchema: z.ZodMiniType<
  TriggerEnvRequirement,
  unknown
>;
export declare function triggerEnvRequirementFromJSON(
  jsonString: string,
): SafeParseResult<TriggerEnvRequirement, SDKValidationError>;
//# sourceMappingURL=triggerenvrequirement.d.ts.map
