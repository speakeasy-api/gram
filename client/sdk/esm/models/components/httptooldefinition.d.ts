import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { CanonicalToolAttributes } from "./canonicaltoolattributes.js";
import { ResponseFilter } from "./responsefilter.js";
import { ToolAnnotations } from "./toolannotations.js";
import { ToolVariation } from "./toolvariation.js";
/**
 * An HTTP tool
 */
export type HTTPToolDefinition = {
  /**
   * Tool annotations providing behavioral hints about the tool
   */
  annotations?: ToolAnnotations | undefined;
  /**
   * The ID of the asset
   */
  assetId: string;
  /**
   * The original details of a tool
   */
  canonical?: CanonicalToolAttributes | undefined;
  /**
   * The canonical name of the tool. Will be the same as the name if there is no variation.
   */
  canonicalName: string;
  /**
   * Confirmation mode for the tool
   */
  confirm?: string | undefined;
  /**
   * Prompt for the confirmation
   */
  confirmPrompt?: string | undefined;
  /**
   * The creation date of the tool.
   */
  createdAt: Date;
  /**
   * The default server URL for the tool
   */
  defaultServerUrl?: string | undefined;
  /**
   * The ID of the deployment
   */
  deploymentId: string;
  /**
   * Description of the tool
   */
  description: string;
  /**
   * HTTP method for the request
   */
  httpMethod: string;
  /**
   * The ID of the tool
   */
  id: string;
  /**
   * The name of the tool
   */
  name: string;
  /**
   * The ID of the OpenAPI v3 document
   */
  openapiv3DocumentId?: string | undefined;
  /**
   * OpenAPI v3 operation
   */
  openapiv3Operation?: string | undefined;
  /**
   * The name of the source package
   */
  packageName?: string | undefined;
  /**
   * Path for the request
   */
  path: string;
  /**
   * The ID of the project
   */
  projectId: string;
  /**
   * Response filter metadata for the tool
   */
  responseFilter?: ResponseFilter | undefined;
  /**
   * JSON schema for the request
   */
  schema: string;
  /**
   * Version of the schema
   */
  schemaVersion?: string | undefined;
  /**
   * Security requirements for the underlying HTTP endpoint
   */
  security?: string | undefined;
  /**
   * Summarizer for the tool
   */
  summarizer?: string | undefined;
  /**
   * Summary of the tool
   */
  summary: string;
  /**
   * The tags list for this http tool
   */
  tags: Array<string>;
  /**
   * The URN of this tool
   */
  toolUrn: string;
  /**
   * The last update date of the tool.
   */
  updatedAt: Date;
  variation?: ToolVariation | undefined;
};
/** @internal */
export declare const HTTPToolDefinition$inboundSchema: z.ZodMiniType<
  HTTPToolDefinition,
  unknown
>;
export declare function httpToolDefinitionFromJSON(
  jsonString: string,
): SafeParseResult<HTTPToolDefinition, SDKValidationError>;
//# sourceMappingURL=httptooldefinition.d.ts.map
