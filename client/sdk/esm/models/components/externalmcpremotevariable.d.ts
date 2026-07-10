import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A URL template variable for a remote MCP server
 */
export type ExternalMCPRemoteVariable = {
  /**
   * Allowed values for the variable
   */
  choices?: Array<string> | undefined;
  /**
   * Default value for the variable
   */
  default?: string | undefined;
  /**
   * Description of the variable
   */
  description?: string | undefined;
  /**
   * Whether this variable is required
   */
  isRequired?: boolean | undefined;
  /**
   * Whether this variable value should be treated as secret
   */
  isSecret?: boolean | undefined;
};
/** @internal */
export declare const ExternalMCPRemoteVariable$inboundSchema: z.ZodMiniType<
  ExternalMCPRemoteVariable,
  unknown
>;
export declare function externalMCPRemoteVariableFromJSON(
  jsonString: string,
): SafeParseResult<ExternalMCPRemoteVariable, SDKValidationError>;
//# sourceMappingURL=externalmcpremotevariable.d.ts.map
