import * as z from "zod/v4-mini";
export type AddFunctionsForm = {
  /**
   * The ID of the functions file from the assets service.
   */
  assetId: string;
  /**
   * The amount of memory in MiB to allocate for the function (1 MiB = 1024 * 1024 bytes).
   */
  memoryMib?: number | undefined;
  /**
   * The functions file display name.
   */
  name: string;
  /**
   * The runtime to use when executing functions. Allowed values are: nodejs:22, nodejs:24, python:3.12.
   */
  runtime: string;
  /**
   * The number of instances to scale the function to.
   */
  scale?: number | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
};
/** @internal */
export type AddFunctionsForm$Outbound = {
  asset_id: string;
  memory_mib?: number | undefined;
  name: string;
  runtime: string;
  scale?: number | undefined;
  slug: string;
};
/** @internal */
export declare const AddFunctionsForm$outboundSchema: z.ZodMiniType<
  AddFunctionsForm$Outbound,
  AddFunctionsForm
>;
export declare function addFunctionsFormToJSON(
  addFunctionsForm: AddFunctionsForm,
): string;
//# sourceMappingURL=addfunctionsform.d.ts.map
