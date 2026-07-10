import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export declare const UpdatePluginServerFormPolicy: {
    readonly Required: "required";
    readonly Optional: "optional";
};
export type UpdatePluginServerFormPolicy = ClosedEnum<typeof UpdatePluginServerFormPolicy>;
export type UpdatePluginServerForm = {
    displayName: string;
    id: string;
    pluginId: string;
    policy?: UpdatePluginServerFormPolicy | undefined;
    sortOrder?: number | undefined;
};
/** @internal */
export declare const UpdatePluginServerFormPolicy$outboundSchema: z.ZodMiniEnum<typeof UpdatePluginServerFormPolicy>;
/** @internal */
export type UpdatePluginServerForm$Outbound = {
    display_name: string;
    id: string;
    plugin_id: string;
    policy: string;
    sort_order: number;
};
/** @internal */
export declare const UpdatePluginServerForm$outboundSchema: z.ZodMiniType<UpdatePluginServerForm$Outbound, UpdatePluginServerForm>;
export declare function updatePluginServerFormToJSON(updatePluginServerForm: UpdatePluginServerForm): string;
//# sourceMappingURL=updatepluginserverform.d.ts.map