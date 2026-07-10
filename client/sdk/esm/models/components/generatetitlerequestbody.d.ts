import * as z from "zod/v4-mini";
export type GenerateTitleRequestBody = {
    /**
     * The ID of the chat
     */
    id: string;
    /**
     * When present, sets the chat's title manually (empty string resets to auto-generated). When omitted, the current title is returned without changes.
     */
    title?: string | undefined;
};
/** @internal */
export type GenerateTitleRequestBody$Outbound = {
    id: string;
    title?: string | undefined;
};
/** @internal */
export declare const GenerateTitleRequestBody$outboundSchema: z.ZodMiniType<GenerateTitleRequestBody$Outbound, GenerateTitleRequestBody>;
export declare function generateTitleRequestBodyToJSON(generateTitleRequestBody: GenerateTitleRequestBody): string;
//# sourceMappingURL=generatetitlerequestbody.d.ts.map