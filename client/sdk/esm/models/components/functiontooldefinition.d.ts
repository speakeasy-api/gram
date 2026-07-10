import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { CanonicalToolAttributes } from "./canonicaltoolattributes.js";
import { ToolAnnotations } from "./toolannotations.js";
import { ToolVariation } from "./toolvariation.js";
/**
 * A function tool
 */
export type FunctionToolDefinition = {
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
   * The ID of the deployment
   */
  deploymentId: string;
  /**
   * Description of the tool
   */
  description: string;
  /**
   * The ID of the function
   */
  functionId: string;
  /**
   * The ID of the tool
   */
  id: string;
  /**
   * Meta tags for the tool
   */
  meta?:
    | {
        [k: string]: any;
      }
    | undefined;
  /**
   * The name of the tool
   */
  name: string;
  /**
   * The ID of the project
   */
  projectId: string;
  /**
   * Runtime environment (e.g., nodejs:24, python:3.12)
   */
  runtime: string;
  /**
   * JSON schema for the request
   */
  schema: string;
  /**
   * Version of the schema
   */
  schemaVersion?: string | undefined;
  /**
   * Summarizer for the tool
   */
  summarizer?: string | undefined;
  /**
   * The tags list for this function tool
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
  /**
   * Variables configuration for the function
   */
  variables?: any | undefined;
  variation?: ToolVariation | undefined;
};
/** @internal */
export declare const FunctionToolDefinition$inboundSchema: z.ZodMiniType<
  FunctionToolDefinition,
  unknown
>;
export declare function functionToolDefinitionFromJSON(
  jsonString: string,
): SafeParseResult<FunctionToolDefinition, SDKValidationError>;
//# sourceMappingURL=functiontooldefinition.d.ts.map
