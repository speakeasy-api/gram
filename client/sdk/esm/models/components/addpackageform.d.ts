import * as z from "zod/v4-mini";
export type AddPackageForm = {
  /**
   * The name of the package to add.
   */
  name: string;
  /**
   * The version of the package to add. If omitted, the latest version will be used.
   */
  version?: string | undefined;
};
/** @internal */
export type AddPackageForm$Outbound = {
  name: string;
  version?: string | undefined;
};
/** @internal */
export declare const AddPackageForm$outboundSchema: z.ZodMiniType<
  AddPackageForm$Outbound,
  AddPackageForm
>;
export declare function addPackageFormToJSON(
  addPackageForm: AddPackageForm,
): string;
//# sourceMappingURL=addpackageform.d.ts.map
