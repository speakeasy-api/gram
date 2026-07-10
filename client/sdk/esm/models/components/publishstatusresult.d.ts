import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type PublishStatusResult = {
    /**
     * Whether GitHub publishing is configured on the server.
     */
    configured: boolean;
    /**
     * Whether this project has a GitHub connection.
     */
    connected: boolean;
    /**
     * When the project was last published to GitHub. Absent when the project is not connected.
     */
    lastPublishedAt?: Date | undefined;
    /**
     * Git-based Claude Code marketplace URL — the value to pass to `/plugin marketplace add` or set as the source URL in `extraKnownMarketplaces`. Present once a marketplace token has been minted, which happens automatically on the first publish.
     */
    marketplaceUrl?: string | undefined;
    /**
     * GitHub repo name, if connected.
     */
    repoName?: string | undefined;
    /**
     * GitHub repo owner, if connected.
     */
    repoOwner?: string | undefined;
    /**
     * Full GitHub repository URL, if connected.
     */
    repoUrl?: string | undefined;
    /**
     * Whether the project's current plugin state matches what was last published to GitHub. Absent when the project is not connected, or when the connection predates content fingerprinting (freshness can't be determined).
     */
    upToDate?: boolean | undefined;
};
/** @internal */
export declare const PublishStatusResult$inboundSchema: z.ZodMiniType<PublishStatusResult, unknown>;
export declare function publishStatusResultFromJSON(jsonString: string): SafeParseResult<PublishStatusResult, SDKValidationError>;
//# sourceMappingURL=publishstatusresult.d.ts.map