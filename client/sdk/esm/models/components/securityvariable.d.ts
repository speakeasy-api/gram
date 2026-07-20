import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SecurityVariable = {
  /**
   * The bearer format
   */
  bearerFormat?: string | undefined;
  /**
   * User-friendly display name for the security variable (defaults to name if not set)
   */
  displayName?: string | undefined;
  /**
   * The environment variables
   */
  envVariables: Array<string>;
  /**
   * The unique identifier of the security variable
   */
  id: string;
  /**
   * Where the security token is placed
   */
  inPlacement: string;
  /**
   * The name of the security scheme (actual header/parameter name)
   */
  name: string;
  /**
   * The OAuth flows
   */
  oauthFlows?: Uint8Array | string | undefined;
  /**
   * The OAuth types
   */
  oauthTypes?: Array<string> | undefined;
  /**
   * The security scheme
   */
  scheme: string;
  /**
   * The type of security
   */
  type?: string | undefined;
};
/** @internal */
export declare const SecurityVariable$inboundSchema: z.ZodMiniType<
  SecurityVariable,
  unknown
>;
export declare function securityVariableFromJSON(
  jsonString: string,
): SafeParseResult<SecurityVariable, SDKValidationError>;
//# sourceMappingURL=securityvariable.d.ts.map
