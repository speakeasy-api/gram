import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { DeploymentExternalMCP } from "./deploymentexternalmcp.js";
import { DeploymentFunctions } from "./deploymentfunctions.js";
import { DeploymentPackage } from "./deploymentpackage.js";
import { OpenAPIv3DeploymentAsset } from "./openapiv3deploymentasset.js";
export type GetDeploymentResult = {
    /**
     * The ID of the deployment that this deployment was cloned from.
     */
    clonedFrom?: string | undefined;
    /**
     * The creation date of the deployment.
     */
    createdAt: Date;
    /**
     * The external ID to refer to the deployment. This can be a git commit hash for example.
     */
    externalId?: string | undefined;
    /**
     * The number of tools in the deployment generated from external MCP servers.
     */
    externalMcpToolCount: number;
    /**
     * The external MCP servers that were deployed.
     */
    externalMcps?: Array<DeploymentExternalMCP> | undefined;
    /**
     * The upstream URL a deployment can refer to. This can be a github url to a commit hash or pull request.
     */
    externalUrl?: string | undefined;
    /**
     * The IDs, as returned from the assets upload service, to uploaded OpenAPI 3.x documents whose operations will become tool definitions.
     */
    functionsAssets?: Array<DeploymentFunctions> | undefined;
    /**
     * The number of tools in the deployment generated from Functions.
     */
    functionsToolCount: number;
    /**
     * The github pull request that resulted in the deployment.
     */
    githubPr?: string | undefined;
    /**
     * The github repository in the form of "owner/repo".
     */
    githubRepo?: string | undefined;
    /**
     * The commit hash that triggered the deployment.
     */
    githubSha?: string | undefined;
    /**
     * The ID to of the deployment.
     */
    id: string;
    /**
     * A unique identifier that will mitigate against duplicate deployments.
     */
    idempotencyKey?: string | undefined;
    /**
     * The IDs, as returned from the assets upload service, to uploaded OpenAPI 3.x documents whose operations will become tool definitions.
     */
    openapiv3Assets: Array<OpenAPIv3DeploymentAsset>;
    /**
     * The number of tools in the deployment generated from OpenAPI documents.
     */
    openapiv3ToolCount: number;
    /**
     * The ID of the organization that the deployment belongs to.
     */
    organizationId: string;
    /**
     * The packages that were deployed.
     */
    packages: Array<DeploymentPackage>;
    /**
     * The ID of the project that the deployment belongs to.
     */
    projectId: string;
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
export declare const GetDeploymentResult$inboundSchema: z.ZodMiniType<GetDeploymentResult, unknown>;
export declare function getDeploymentResultFromJSON(jsonString: string): SafeParseResult<GetDeploymentResult, SDKValidationError>;
//# sourceMappingURL=getdeploymentresult.d.ts.map