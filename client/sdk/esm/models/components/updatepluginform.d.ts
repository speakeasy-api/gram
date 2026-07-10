import * as z from "zod/v4-mini";
export type UpdatePluginForm = {
    /**
     * Updated description.
     */
    description?: string | undefined;
    id: string;
    /**
     * Updated display name.
     */
    name: string;
    /**
     * Updated slug.
     */
    slug: string;
};
/** @internal */
export type UpdatePluginForm$Outbound = {
    description?: string | undefined;
    id: string;
    name: string;
    slug: string;
};
/** @internal */
export declare const UpdatePluginForm$outboundSchema: z.ZodMiniType<UpdatePluginForm$Outbound, UpdatePluginForm>;
export declare function updatePluginFormToJSON(updatePluginForm: UpdatePluginForm): string;
//# sourceMappingURL=updatepluginform.d.ts.map