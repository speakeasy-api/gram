import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DeploymentExternalMCP = {
  /**
   * The ID of the deployment external MCP record.
   */
  id: string;
  /**
   * The display name for the external MCP server.
   */
  name: string;
  /**
   * The ID of the internal collection registry the server is from.
   */
  organizationMcpCollectionRegistryId?: string | undefined;
  /**
   * The ID of the external MCP registry the server is from.
   */
  registryId?: string | undefined;
  /**
   * The canonical server name used to look up the server in the registry.
   */
  registryServerSpecifier: string;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
};
/** @internal */
export declare const DeploymentExternalMCP$inboundSchema: z.ZodMiniType<
  DeploymentExternalMCP,
  unknown
>;
export declare function deploymentExternalMCPFromJSON(
  jsonString: string,
): SafeParseResult<DeploymentExternalMCP, SDKValidationError>;
//# sourceMappingURL=deploymentexternalmcp.d.ts.map
