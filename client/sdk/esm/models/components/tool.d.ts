import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ExternalMCPToolDefinition } from "./externalmcptooldefinition.js";
import { FunctionToolDefinition } from "./functiontooldefinition.js";
import { HTTPToolDefinition } from "./httptooldefinition.js";
import { PlatformToolDefinition } from "./platformtooldefinition.js";
import { PromptTemplate } from "./prompttemplate.js";
/**
 * A polymorphic tool - can be an HTTP tool, function tool, prompt template, or external MCP proxy
 */
export type Tool = {
  /**
   * A proxy tool that references an external MCP server
   */
  externalMcpToolDefinition?: ExternalMCPToolDefinition | undefined;
  /**
   * A function tool
   */
  functionToolDefinition?: FunctionToolDefinition | undefined;
  /**
   * An HTTP tool
   */
  httpToolDefinition?: HTTPToolDefinition | undefined;
  /**
   * A platform-owned tool served directly by the platform
   */
  platformToolDefinition?: PlatformToolDefinition | undefined;
  /**
   * A prompt template
   */
  promptTemplate?: PromptTemplate | undefined;
};
/** @internal */
export declare const Tool$inboundSchema: z.ZodMiniType<Tool, unknown>;
export declare function toolFromJSON(
  jsonString: string,
): SafeParseResult<Tool, SDKValidationError>;
//# sourceMappingURL=tool.d.ts.map
