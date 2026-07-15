import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DeploymentFunctions = {
  /**
   * The ID of the uploaded asset.
   */
  assetId: string;
  /**
   * The ID of the deployment asset.
   */
  id: string;
  /**
   * The memory limit in MiB of function runner machines.
   */
  memoryMib?: number | undefined;
  /**
   * The name to give the document as it will be displayed in UIs.
   */
  name: string;
  /**
   * The runtime to use when executing functions.
   */
  runtime: string;
  /**
   * The number of instances to run for the function.
   */
  scale?: number | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
};
/** @internal */
export declare const DeploymentFunctions$inboundSchema: z.ZodMiniType<
  DeploymentFunctions,
  unknown
>;
export declare function deploymentFunctionsFromJSON(
  jsonString: string,
): SafeParseResult<DeploymentFunctions, SDKValidationError>;
//# sourceMappingURL=deploymentfunctions.d.ts.map
