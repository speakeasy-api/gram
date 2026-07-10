import * as z from "zod/v4-mini";
export type SetPinnedRequestBody = {
    /**
     * The ID of the chat to pin or unpin
     */
    id: string;
    /**
     * True to pin the chat, false to unpin it
     */
    pinned: boolean;
};
/** @internal */
export type SetPinnedRequestBody$Outbound = {
    id: string;
    pinned: boolean;
};
/** @internal */
export declare const SetPinnedRequestBody$outboundSchema: z.ZodMiniType<SetPinnedRequestBody$Outbound, SetPinnedRequestBody>;
export declare function setPinnedRequestBodyToJSON(setPinnedRequestBody: SetPinnedRequestBody): string;
//# sourceMappingURL=setpinnedrequestbody.d.ts.map