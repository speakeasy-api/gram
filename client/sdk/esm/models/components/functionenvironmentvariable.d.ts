import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type FunctionEnvironmentVariable = {
  /**
   * Optional value of the function variable comes from a specific auth input
   */
  authInputType?: string | undefined;
  /**
   * Description of the function environment variable
   */
  description?: string | undefined;
  /**
   * The environment variables
   */
  name: string;
};
/** @internal */
export declare const FunctionEnvironmentVariable$inboundSchema: z.ZodMiniType<
  FunctionEnvironmentVariable,
  unknown
>;
export declare function functionEnvironmentVariableFromJSON(
  jsonString: string,
): SafeParseResult<FunctionEnvironmentVariable, SDKValidationError>;
//# sourceMappingURL=functionenvironmentvariable.d.ts.map
