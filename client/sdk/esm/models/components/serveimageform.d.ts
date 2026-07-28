import * as z from "zod/v4-mini";
export type ServeImageForm = {
  /**
   * The ID of the asset to serve
   */
  id: string;
};
/** @internal */
export type ServeImageForm$Outbound = {
  id: string;
};
/** @internal */
export declare const ServeImageForm$outboundSchema: z.ZodMiniType<
  ServeImageForm$Outbound,
  ServeImageForm
>;
export declare function serveImageFormToJSON(
  serveImageForm: ServeImageForm,
): string;
//# sourceMappingURL=serveimageform.d.ts.map
