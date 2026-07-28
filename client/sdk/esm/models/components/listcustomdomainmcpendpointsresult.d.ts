import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { CustomDomainMcpEndpoint } from "./customdomainmcpendpoint.js";
/**
 * Result of listing the MCP endpoints registered under an organization's custom domain.
 */
export type ListCustomDomainMcpEndpointsResult = {
  mcpEndpoints: Array<CustomDomainMcpEndpoint>;
};
/** @internal */
export declare const ListCustomDomainMcpEndpointsResult$inboundSchema: z.ZodMiniType<
  ListCustomDomainMcpEndpointsResult,
  unknown
>;
export declare function listCustomDomainMcpEndpointsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListCustomDomainMcpEndpointsResult, SDKValidationError>;
//# sourceMappingURL=listcustomdomainmcpendpointsresult.d.ts.map
