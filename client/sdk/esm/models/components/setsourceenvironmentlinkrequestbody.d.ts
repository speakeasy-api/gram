import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The kind of source (http or function)
 */
export declare const SourceKind: {
    readonly Http: "http";
    readonly Function: "function";
};
/**
 * The kind of source (http or function)
 */
export type SourceKind = ClosedEnum<typeof SourceKind>;
export type SetSourceEnvironmentLinkRequestBody = {
    /**
     * The ID of the environment to link
     */
    environmentId: string;
    /**
     * The kind of source (http or function)
     */
    sourceKind: SourceKind;
    /**
     * The slug of the source
     */
    sourceSlug: string;
};
/** @internal */
export declare const SourceKind$outboundSchema: z.ZodMiniEnum<typeof SourceKind>;
/** @internal */
export type SetSourceEnvironmentLinkRequestBody$Outbound = {
    environment_id: string;
    source_kind: string;
    source_slug: string;
};
/** @internal */
export declare const SetSourceEnvironmentLinkRequestBody$outboundSchema: z.ZodMiniType<SetSourceEnvironmentLinkRequestBody$Outbound, SetSourceEnvironmentLinkRequestBody>;
export declare function setSourceEnvironmentLinkRequestBodyToJSON(setSourceEnvironmentLinkRequestBody: SetSourceEnvironmentLinkRequestBody): string;
//# sourceMappingURL=setsourceenvironmentlinkrequestbody.d.ts.map