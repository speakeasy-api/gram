import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPHeaderDefinition } from "./externalmcpheaderdefinition.js";
import { FunctionEnvironmentVariable } from "./functionenvironmentvariable.js";
import { PromptTemplateEntry } from "./prompttemplateentry.js";
import { ResourceEntry } from "./resourceentry.js";
import { SecurityVariable } from "./securityvariable.js";
import { ServerVariable } from "./servervariable.js";
import { ToolEntry } from "./toolentry.js";
import { ToolsetOrigin } from "./toolsetorigin.js";
export type ToolsetEntry = {
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
    promptTemplates: Array<PromptTemplateEntry>;
    /**
     * The resource URNs in this toolset
     */
    resourceUrns: Array<string>;
    /**
     * The resources in this toolset
     */
    resources: Array<ResourceEntry>;
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
     * The tools in this toolset
     */
    tools: Array<ToolEntry>;
    /**
     * When the toolset was last updated.
     */
    updatedAt: Date;
};
/** @internal */
export declare const ToolsetEntry$inboundSchema: z.ZodMiniType<ToolsetEntry, unknown>;
export declare function toolsetEntryFromJSON(jsonString: string): SafeParseResult<ToolsetEntry, SDKValidationError>;
//# sourceMappingURL=toolsetentry.d.ts.map