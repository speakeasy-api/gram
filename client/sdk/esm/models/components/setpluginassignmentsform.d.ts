import * as z from "zod/v4-mini";
export type SetPluginAssignmentsForm = {
  pluginId: string;
  /**
   * List of principal URNs to assign.
   */
  principalUrns: Array<string>;
};
/** @internal */
export type SetPluginAssignmentsForm$Outbound = {
  plugin_id: string;
  principal_urns: Array<string>;
};
/** @internal */
export declare const SetPluginAssignmentsForm$outboundSchema: z.ZodMiniType<
  SetPluginAssignmentsForm$Outbound,
  SetPluginAssignmentsForm
>;
export declare function setPluginAssignmentsFormToJSON(
  setPluginAssignmentsForm: SetPluginAssignmentsForm,
): string;
//# sourceMappingURL=setpluginassignmentsform.d.ts.map
