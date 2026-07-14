import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPHeaderDefinition } from "./externalmcpheaderdefinition.js";
import { FunctionEnvironmentVariable } from "./functionenvironmentvariable.js";
import { InstanceMcpServer } from "./instancemcpserver.js";
import { PromptTemplate } from "./prompttemplate.js";
import { SecurityVariable } from "./securityvariable.js";
import { ServerVariable } from "./servervariable.js";
import { Tool } from "./tool.js";
export type GetInstanceResult = {
  /**
   * The description of the toolset
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
   * The MCP servers that are relevant to the toolset
   */
  mcpServers: Array<InstanceMcpServer>;
  /**
   * The name of the toolset
   */
  name: string;
  /**
   * The list of prompt templates
   */
  promptTemplates?: Array<PromptTemplate> | undefined;
  /**
   * The security variables that are relevant to the toolset
   */
  securityVariables?: Array<SecurityVariable> | undefined;
  /**
   * The server variables that are relevant to the toolset
   */
  serverVariables?: Array<ServerVariable> | undefined;
  /**
   * The list of tools
   */
  tools: Array<Tool>;
};
/** @internal */
export declare const GetInstanceResult$inboundSchema: z.ZodMiniType<
  GetInstanceResult,
  unknown
>;
export declare function getInstanceResultFromJSON(
  jsonString: string,
): SafeParseResult<GetInstanceResult, SDKValidationError>;
//# sourceMappingURL=getinstanceresult.d.ts.map
