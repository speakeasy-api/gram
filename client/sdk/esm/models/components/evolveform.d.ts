import * as z from "zod/v4-mini";
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
import { AddPackageForm, AddPackageForm$Outbound } from "./addpackageform.js";
export type EvolveForm = {
  /**
   * The ID of the deployment to evolve. If omitted, the latest deployment will be used.
   */
  deploymentId?: string | undefined;
  /**
   * The external MCP servers, identified by slug, to exclude from the new deployment when cloning a previous deployment.
   */
  excludeExternalMcps?: Array<string> | undefined;
  /**
   * The functions, identified by slug, to exclude from the new deployment when cloning a previous deployment.
   */
  excludeFunctions?: Array<string> | undefined;
  /**
   * The OpenAPI 3.x documents, identified by slug, to exclude from the new deployment when cloning a previous deployment.
   */
  excludeOpenapiv3Assets?: Array<string> | undefined;
  /**
   * The packages to exclude from the new deployment when cloning a previous deployment.
   */
  excludePackages?: Array<string> | undefined;
  /**
   * If true, the deployment will be created in non-blocking mode where the request will return immediately and the deployment will proceed asynchronously.
   */
  nonBlocking?: boolean | undefined;
  /**
   * The external MCP servers to upsert in the new deployment.
   */
  upsertExternalMcps?: Array<AddExternalMCPForm> | undefined;
  /**
   * The tool functions to upsert in the new deployment.
   */
  upsertFunctions?: Array<AddFunctionsForm> | undefined;
  /**
   * The OpenAPI 3.x documents to upsert in the new deployment.
   */
  upsertOpenapiv3Assets?: Array<AddOpenAPIv3DeploymentAssetForm> | undefined;
  /**
   * The packages to upsert in the new deployment.
   */
  upsertPackages?: Array<AddPackageForm> | undefined;
};
/** @internal */
export type EvolveForm$Outbound = {
  deployment_id?: string | undefined;
  exclude_external_mcps?: Array<string> | undefined;
  exclude_functions?: Array<string> | undefined;
  exclude_openapiv3_assets?: Array<string> | undefined;
  exclude_packages?: Array<string> | undefined;
  non_blocking?: boolean | undefined;
  upsert_external_mcps?: Array<AddExternalMCPForm$Outbound> | undefined;
  upsert_functions?: Array<AddFunctionsForm$Outbound> | undefined;
  upsert_openapiv3_assets?:
    | Array<AddOpenAPIv3DeploymentAssetForm$Outbound>
    | undefined;
  upsert_packages?: Array<AddPackageForm$Outbound> | undefined;
};
/** @internal */
export declare const EvolveForm$outboundSchema: z.ZodMiniType<
  EvolveForm$Outbound,
  EvolveForm
>;
export declare function evolveFormToJSON(evolveForm: EvolveForm): string;
//# sourceMappingURL=evolveform.d.ts.map
