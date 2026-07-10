import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Name of the feature to update
 */
export declare const FeatureName: {
    readonly Logs: "logs";
    readonly ToolIoLogs: "tool_io_logs";
    readonly SessionCapture: "session_capture";
    readonly AuthzChallengeLogging: "authz_challenge_logging";
    readonly Webhooks: "webhooks";
    readonly Sso: "sso";
    readonly Scim: "scim";
    readonly ObservabilityMode: "observability_mode";
};
/**
 * Name of the feature to update
 */
export type FeatureName = ClosedEnum<typeof FeatureName>;
export type SetProductFeatureRequestBody = {
    /**
     * Whether the feature should be enabled
     */
    enabled: boolean;
    /**
     * Name of the feature to update
     */
    featureName: FeatureName;
};
/** @internal */
export declare const FeatureName$outboundSchema: z.ZodMiniEnum<typeof FeatureName>;
/** @internal */
export type SetProductFeatureRequestBody$Outbound = {
    enabled: boolean;
    feature_name: string;
};
/** @internal */
export declare const SetProductFeatureRequestBody$outboundSchema: z.ZodMiniType<SetProductFeatureRequestBody$Outbound, SetProductFeatureRequestBody>;
export declare function setProductFeatureRequestBodyToJSON(setProductFeatureRequestBody: SetProductFeatureRequestBody): string;
//# sourceMappingURL=setproductfeaturerequestbody.d.ts.map