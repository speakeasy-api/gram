import { HTTPClient } from "./http.js";
import { Logger } from "./logger.js";
import { RetryConfig } from "./retries.js";
/**
 * Contains the list of servers available to the SDK
 */
export declare const ServerList: readonly ["https://app.getgram.ai"];
export type SDKOptions = {
    httpClient?: HTTPClient;
    /**
     * Allows overriding the default server used by the SDK
     */
    serverIdx?: number | undefined;
    /**
     * Allows overriding the default server URL used by the SDK
     */
    serverURL?: string | undefined;
    /**
     * Allows overriding the default user agent used by the SDK
     */
    userAgent?: string | undefined;
    /**
     * Allows overriding the default retry config used by the SDK
     */
    retryConfig?: RetryConfig;
    timeoutMs?: number;
    debugLogger?: Logger;
};
export declare function serverURLFromOptions(options: SDKOptions): URL | null;
export declare const SDK_METADATA: {
    readonly language: "typescript";
    readonly openapiDocVersion: "0.0.1";
    readonly sdkVersion: "0.33.8";
    readonly genVersion: "2.915.0";
    readonly userAgent: "speakeasy-sdk/typescript 0.33.8 2.915.0 0.0.1 @gram/client";
};
//# sourceMappingURL=config.d.ts.map