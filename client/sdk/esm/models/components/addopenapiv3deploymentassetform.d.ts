import * as z from "zod/v4-mini";
export type AddOpenAPIv3DeploymentAssetForm = {
  /**
   * The ID of the uploaded asset.
   */
  assetId: string;
  /**
   * The name to give the document as it will be displayed in UIs.
   */
  name: string;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
};
/** @internal */
export type AddOpenAPIv3DeploymentAssetForm$Outbound = {
  asset_id: string;
  name: string;
  slug: string;
};
/** @internal */
export declare const AddOpenAPIv3DeploymentAssetForm$outboundSchema: z.ZodMiniType<
  AddOpenAPIv3DeploymentAssetForm$Outbound,
  AddOpenAPIv3DeploymentAssetForm
>;
export declare function addOpenAPIv3DeploymentAssetFormToJSON(
  addOpenAPIv3DeploymentAssetForm: AddOpenAPIv3DeploymentAssetForm,
): string;
//# sourceMappingURL=addopenapiv3deploymentassetform.d.ts.map
