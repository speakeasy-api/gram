import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A function resource
 */
export type FunctionResourceDefinition = {
  /**
   * The creation date of the resource.
   */
  createdAt: Date;
  /**
   * The ID of the deployment
   */
  deploymentId: string;
  /**
   * Description of the resource
   */
  description: string;
  /**
   * The ID of the function
   */
  functionId: string;
  /**
   * The ID of the resource
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
   * Optional MIME type of the resource
   */
  mimeType?: string | undefined;
  /**
   * The name of the resource
   */
  name: string;
  /**
   * The ID of the project
   */
  projectId: string;
  /**
   * The URN of this resource
   */
  resourceUrn: string;
  /**
   * Runtime environment (e.g., nodejs:24, python:3.12)
   */
  runtime: string;
  /**
   * Optional title for the resource
   */
  title?: string | undefined;
  /**
   * The last update date of the resource.
   */
  updatedAt: Date;
  /**
   * The URI of the resource
   */
  uri: string;
  /**
   * Variables configuration for the resource
   */
  variables?: any | undefined;
};
/** @internal */
export declare const FunctionResourceDefinition$inboundSchema: z.ZodMiniType<
  FunctionResourceDefinition,
  unknown
>;
export declare function functionResourceDefinitionFromJSON(
  jsonString: string,
): SafeParseResult<FunctionResourceDefinition, SDKValidationError>;
//# sourceMappingURL=functionresourcedefinition.d.ts.map
