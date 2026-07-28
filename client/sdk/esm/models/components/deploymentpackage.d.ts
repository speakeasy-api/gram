import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DeploymentPackage = {
  /**
   * The ID of the deployment package.
   */
  id: string;
  /**
   * The name of the package.
   */
  name: string;
  /**
   * The version of the package.
   */
  version: string;
};
/** @internal */
export declare const DeploymentPackage$inboundSchema: z.ZodMiniType<
  DeploymentPackage,
  unknown
>;
export declare function deploymentPackageFromJSON(
  jsonString: string,
): SafeParseResult<DeploymentPackage, SDKValidationError>;
//# sourceMappingURL=deploymentpackage.d.ts.map
