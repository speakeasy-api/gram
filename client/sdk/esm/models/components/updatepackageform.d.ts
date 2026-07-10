import * as z from "zod/v4-mini";
export type UpdatePackageForm = {
  /**
   * The description of the package. Limited markdown syntax is supported.
   */
  description?: string | undefined;
  /**
   * The id of the package to update
   */
  id: string;
  /**
   * The asset ID of the image to show for this package
   */
  imageAssetId?: string | undefined;
  /**
   * The keywords of the package
   */
  keywords?: Array<string> | undefined;
  /**
   * The summary of the package
   */
  summary?: string | undefined;
  /**
   * The title of the package
   */
  title?: string | undefined;
  /**
   * External URL for the package owner
   */
  url?: string | undefined;
};
/** @internal */
export type UpdatePackageForm$Outbound = {
  description?: string | undefined;
  id: string;
  image_asset_id?: string | undefined;
  keywords?: Array<string> | undefined;
  summary?: string | undefined;
  title?: string | undefined;
  url?: string | undefined;
};
/** @internal */
export declare const UpdatePackageForm$outboundSchema: z.ZodMiniType<
  UpdatePackageForm$Outbound,
  UpdatePackageForm
>;
export declare function updatePackageFormToJSON(
  updatePackageForm: UpdatePackageForm,
): string;
//# sourceMappingURL=updatepackageform.d.ts.map
