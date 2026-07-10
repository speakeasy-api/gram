import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DeploymentSummary = {
    /**
     * The creation date of the deployment.
     */
    createdAt: Date;
    /**
     * The number of external MCP server assets.
     */
    externalMcpAssetCount: number;
    /**
     * The number of tools in the deployment generated from external MCP servers.
     */
    externalMcpToolCount: number;
    /**
     * The number of Functions assets.
     */
    functionsAssetCount: number;
    /**
     * The number of tools in the deployment generated from Functions.
     */
    functionsToolCount: number;
    /**
     * The ID to of the deployment.
     */
    id: string;
    /**
     * The number of upstream OpenAPI assets.
     */
    openapiv3AssetCount: number;
    /**
     * The number of tools in the deployment generated from OpenAPI documents.
     */
    openapiv3ToolCount: number;
    /**
     * The status of the deployment.
     */
    status: string;
    /**
     * The ID of the user that created the deployment.
     */
    userId: string;
};
/** @internal */
export declare const DeploymentSummary$inboundSchema: z.ZodMiniType<DeploymentSummary, unknown>;
export declare function deploymentSummaryFromJSON(jsonString: string): SafeParseResult<DeploymentSummary, SDKValidationError>;
//# sourceMappingURL=deploymentsummary.d.ts.map