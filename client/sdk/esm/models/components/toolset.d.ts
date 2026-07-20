import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPHeaderDefinition } from "./externalmcpheaderdefinition.js";
import { ExternalOAuthServer } from "./externaloauthserver.js";
import { FunctionEnvironmentVariable } from "./functionenvironmentvariable.js";
import { OAuthEnablementMetadata } from "./oauthenablementmetadata.js";
import { OAuthProxyServer } from "./oauthproxyserver.js";
import { PromptTemplate } from "./prompttemplate.js";
import { Resource } from "./resource.js";
import { SecurityVariable } from "./securityvariable.js";
import { ServerVariable } from "./servervariable.js";
import { Tool } from "./tool.js";
import { ToolsetOrigin } from "./toolsetorigin.js";
export type Toolset = {
  /**
   * The account type of the organization
   */
  accountType: string;
  /**
   * When the toolset was created.
   */
  createdAt: Date;
  /**
   * The ID of the custom domain to use for the toolset
   */
  customDomainId?: string | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  defaultEnvironmentSlug?: string | undefined;
  /**
   * Description of the toolset
   */
  description?: string | undefined;
  /**
   * The external MCP header definitions that are relevant to the toolset
   */
  externalMcpHeaderDefinitions?: Array<ExternalMCPHeaderDefinition> | undefined;
  externalOauthServer?: ExternalOAuthServer | undefined;
  /**
   * The function environment variables that are relevant to the toolset
   */
  functionEnvironmentVariables?: Array<FunctionEnvironmentVariable> | undefined;
  /**
   * The ID of the toolset
   */
  id: string;
  /**
   * Whether the toolset is enabled for MCP
   */
  mcpEnabled?: boolean | undefined;
  /**
   * Whether the toolset is public in MCP
   */
  mcpIsPublic?: boolean | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  mcpSlug?: string | undefined;
  /**
   * The name of the toolset
   */
  name: string;
  oauthEnablementMetadata: OAuthEnablementMetadata;
  oauthProxyServer?: OAuthProxyServer | undefined;
  /**
   * The organization ID this toolset belongs to
   */
  organizationId: string;
  origin?: ToolsetOrigin | undefined;
  /**
   * The project ID this toolset belongs to
   */
  projectId: string;
  /**
   * The prompt templates in this toolset -- Note: these are actual prompts, as in MCP prompts
   */
  promptTemplates: Array<PromptTemplate>;
  /**
   * The resource URNs in this toolset
   */
  resourceUrns: Array<string>;
  /**
   * The resources in this toolset
   */
  resources: Array<Resource>;
  /**
   * The security variables that are relevant to the toolset
   */
  securityVariables?: Array<SecurityVariable> | undefined;
  /**
   * The server variables that are relevant to the toolset
   */
  serverVariables?: Array<ServerVariable> | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
  /**
   * The mode to use for tool selection
   */
  toolSelectionMode: string;
  /**
   * The tool URNs in this toolset
   */
  toolUrns: Array<string>;
  /**
   * The id of the tool variations group enabling MCP tool filtering for this toolset. Set via toolsets.setToolVariationsGroup; null when filtering is disabled.
   */
  toolVariationsGroupId?: string | undefined;
  /**
   * The tools in this toolset
   */
  tools: Array<Tool>;
  /**
   * The version of the toolset (will be 0 if none exists)
   */
  toolsetVersion: number;
  /**
   * When the toolset was last updated.
   */
  updatedAt: Date;
  /**
   * The id of the user_session_issuer wired to this toolset. Set via toolsets.setUserSessionIssuer; null when no USI is linked.
   */
  userSessionIssuerId?: string | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  userSessionIssuerSlug?: string | undefined;
};
/** @internal */
export declare const Toolset$inboundSchema: z.ZodMiniType<Toolset, unknown>;
export declare function toolsetFromJSON(
  jsonString: string,
): SafeParseResult<Toolset, SDKValidationError>;
//# sourceMappingURL=toolset.d.ts.map
