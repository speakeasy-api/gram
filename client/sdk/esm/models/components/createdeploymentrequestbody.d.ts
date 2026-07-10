import * as z from "zod/v4-mini";
import {
  AddDeploymentPackageForm,
  AddDeploymentPackageForm$Outbound,
} from "./adddeploymentpackageform.js";
import {
  AddExternalMCPForm,
  AddExternalMCPForm$Outbound,
} from "./addexternalmcpform.js";
import {
  AddFunctionsForm,
  AddFunctionsForm$Outbound,
} from "./addfunctionsform.js";
import {
  AddOpenAPIv3DeploymentAssetForm,
  AddOpenAPIv3DeploymentAssetForm$Outbound,
} from "./addopenapiv3deploymentassetform.js";
export type CreateDeploymentRequestBody = {
  /**
   * The external ID to refer to the deployment. This can be a git commit hash for example.
   */
  externalId?: string | undefined;
  externalMcps?: Array<AddExternalMCPForm> | undefined;
  /**
   * The upstream URL a deployment can refer to. This can be a github url to a commit hash or pull request.
   */
  externalUrl?: string | undefined;
  functions?: Array<AddFunctionsForm> | undefined;
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
   * If true, the deployment will be created in non-blocking mode where the request will return immediately and the deployment will proceed asynchronously.
   */
  nonBlocking?: boolean | undefined;
  openapiv3Assets?: Array<AddOpenAPIv3DeploymentAssetForm> | undefined;
  packages?: Array<AddDeploymentPackageForm> | undefined;
};
/** @internal */
export type CreateDeploymentRequestBody$Outbound = {
  external_id?: string | undefined;
  external_mcps?: Array<AddExternalMCPForm$Outbound> | undefined;
  external_url?: string | undefined;
  functions?: Array<AddFunctionsForm$Outbound> | undefined;
  github_pr?: string | undefined;
  github_repo?: string | undefined;
  github_sha?: string | undefined;
  non_blocking?: boolean | undefined;
  openapiv3_assets?:
    | Array<AddOpenAPIv3DeploymentAssetForm$Outbound>
    | undefined;
  packages?: Array<AddDeploymentPackageForm$Outbound> | undefined;
};
/** @internal */
export declare const CreateDeploymentRequestBody$outboundSchema: z.ZodMiniType<
  CreateDeploymentRequestBody$Outbound,
  CreateDeploymentRequestBody
>;
export declare function createDeploymentRequestBodyToJSON(
  createDeploymentRequestBody: CreateDeploymentRequestBody,
): string;
//# sourceMappingURL=createdeploymentrequestbody.d.ts.map
