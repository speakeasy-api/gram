import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SwitchAuthScopesSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type SwitchAuthScopesRequest = {
  /**
   * The organization slug to switch scopes
   */
  organizationId?: string | undefined;
  /**
   * The project id to switch scopes too
   */
  projectId?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
export type SwitchAuthScopesResponse = {
  headers: {
    [k: string]: Array<string>;
  };
};
/** @internal */
export type SwitchAuthScopesSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SwitchAuthScopesSecurity$outboundSchema: z.ZodMiniType<
  SwitchAuthScopesSecurity$Outbound,
  SwitchAuthScopesSecurity
>;
export declare function switchAuthScopesSecurityToJSON(
  switchAuthScopesSecurity: SwitchAuthScopesSecurity,
): string;
/** @internal */
export type SwitchAuthScopesRequest$Outbound = {
  organization_id?: string | undefined;
  project_id?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SwitchAuthScopesRequest$outboundSchema: z.ZodMiniType<
  SwitchAuthScopesRequest$Outbound,
  SwitchAuthScopesRequest
>;
export declare function switchAuthScopesRequestToJSON(
  switchAuthScopesRequest: SwitchAuthScopesRequest,
): string;
/** @internal */
export declare const SwitchAuthScopesResponse$inboundSchema: z.ZodMiniType<
  SwitchAuthScopesResponse,
  unknown
>;
export declare function switchAuthScopesResponseFromJSON(
  jsonString: string,
): SafeParseResult<SwitchAuthScopesResponse, SDKValidationError>;
//# sourceMappingURL=switchauthscopes.d.ts.map
