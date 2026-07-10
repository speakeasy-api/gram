import * as z from "zod/v4-mini";
export type SetToolsetEnvironmentLinkRequestBody = {
    /**
     * The ID of the environment to link
     */
    environmentId: string;
    /**
     * The ID of the toolset
     */
    toolsetId: string;
};
/** @internal */
export type SetToolsetEnvironmentLinkRequestBody$Outbound = {
    environment_id: string;
    toolset_id: string;
};
/** @internal */
export declare const SetToolsetEnvironmentLinkRequestBody$outboundSchema: z.ZodMiniType<SetToolsetEnvironmentLinkRequestBody$Outbound, SetToolsetEnvironmentLinkRequestBody>;
export declare function setToolsetEnvironmentLinkRequestBodyToJSON(setToolsetEnvironmentLinkRequestBody: SetToolsetEnvironmentLinkRequestBody): string;
//# sourceMappingURL=settoolsetenvironmentlinkrequestbody.d.ts.map