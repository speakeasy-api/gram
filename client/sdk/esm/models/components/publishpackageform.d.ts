import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The visibility of the package version
 */
export declare const PublishPackageFormVisibility: {
  readonly Public: "public";
  readonly Private: "private";
};
/**
 * The visibility of the package version
 */
export type PublishPackageFormVisibility = ClosedEnum<
  typeof PublishPackageFormVisibility
>;
export type PublishPackageForm = {
  /**
   * The deployment ID to associate with the package version
   */
  deploymentId: string;
  /**
   * The name of the package
   */
  name: string;
  /**
   * The new semantic version of the package to publish
   */
  version: string;
  /**
   * The visibility of the package version
   */
  visibility: PublishPackageFormVisibility;
};
/** @internal */
export declare const PublishPackageFormVisibility$outboundSchema: z.ZodMiniEnum<
  typeof PublishPackageFormVisibility
>;
/** @internal */
export type PublishPackageForm$Outbound = {
  deployment_id: string;
  name: string;
  version: string;
  visibility: string;
};
/** @internal */
export declare const PublishPackageForm$outboundSchema: z.ZodMiniType<
  PublishPackageForm$Outbound,
  PublishPackageForm
>;
export declare function publishPackageFormToJSON(
  publishPackageForm: PublishPackageForm,
): string;
//# sourceMappingURL=publishpackageform.d.ts.map
