import * as z from "zod/v4-mini";
export type CreatePluginForm = {
  /**
   * Optional description.
   */
  description?: string | undefined;
  /**
   * Display name for the plugin.
   */
  name: string;
  /**
   * Optional URL-safe identifier. Auto-generated from name if omitted.
   */
  slug?: string | undefined;
};
/** @internal */
export type CreatePluginForm$Outbound = {
  description?: string | undefined;
  name: string;
  slug?: string | undefined;
};
/** @internal */
export declare const CreatePluginForm$outboundSchema: z.ZodMiniType<
  CreatePluginForm$Outbound,
  CreatePluginForm
>;
export declare function createPluginFormToJSON(
  createPluginForm: CreatePluginForm,
): string;
//# sourceMappingURL=createpluginform.d.ts.map
