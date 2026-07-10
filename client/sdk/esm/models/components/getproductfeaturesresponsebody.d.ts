import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type GetProductFeaturesResponseBody = {
    /**
     * Whether authz challenge logging to ClickHouse is enabled
     */
    authzChallengeLoggingEnabled: boolean;
    /**
     * Whether logging is enabled
     */
    logsEnabled: boolean;
    /**
     * Whether observability mode is enabled, making generated hook plugins fully non-blocking
     */
    observabilityModeEnabled: boolean;
    /**
     * Whether SCIM/directory sync setup is enabled for the organization
     */
    scimEnabled: boolean;
    /**
     * Whether Claude Code session capture is enabled
     */
    sessionCaptureEnabled: boolean;
    /**
     * Whether SSO setup is enabled for the organization
     */
    ssoEnabled: boolean;
    /**
     * Whether tool I/O logging is enabled
     */
    toolIoLogsEnabled: boolean;
    /**
     * Whether webhooks are enabled
     */
    webhooks: boolean;
};
/** @internal */
export declare const GetProductFeaturesResponseBody$inboundSchema: z.ZodMiniType<GetProductFeaturesResponseBody, unknown>;
export declare function getProductFeaturesResponseBodyFromJSON(jsonString: string): SafeParseResult<GetProductFeaturesResponseBody, SDKValidationError>;
//# sourceMappingURL=getproductfeaturesresponsebody.d.ts.map