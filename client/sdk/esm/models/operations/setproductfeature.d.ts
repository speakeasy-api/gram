import * as z from "zod/v4-mini";
import { SetProductFeatureRequestBody, SetProductFeatureRequestBody$Outbound } from "../components/setproductfeaturerequestbody.js";
export type SetProductFeatureSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type SetProductFeatureRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    setProductFeatureRequestBody: SetProductFeatureRequestBody;
};
/** @internal */
export type SetProductFeatureSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SetProductFeatureSecurity$outboundSchema: z.ZodMiniType<SetProductFeatureSecurity$Outbound, SetProductFeatureSecurity>;
export declare function setProductFeatureSecurityToJSON(setProductFeatureSecurity: SetProductFeatureSecurity): string;
/** @internal */
export type SetProductFeatureRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    SetProductFeatureRequestBody: SetProductFeatureRequestBody$Outbound;
};
/** @internal */
export declare const SetProductFeatureRequest$outboundSchema: z.ZodMiniType<SetProductFeatureRequest$Outbound, SetProductFeatureRequest>;
export declare function setProductFeatureRequestToJSON(setProductFeatureRequest: SetProductFeatureRequest): string;
//# sourceMappingURL=setproductfeature.d.ts.map