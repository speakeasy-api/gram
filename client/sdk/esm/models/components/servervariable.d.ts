import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ServerVariable = {
  /**
   * Description of the server variable
   */
  description: string;
  /**
   * The environment variables
   */
  envVariables: Array<string>;
};
/** @internal */
export declare const ServerVariable$inboundSchema: z.ZodMiniType<
  ServerVariable,
  unknown
>;
export declare function serverVariableFromJSON(
  jsonString: string,
): SafeParseResult<ServerVariable, SDKValidationError>;
//# sourceMappingURL=servervariable.d.ts.map
