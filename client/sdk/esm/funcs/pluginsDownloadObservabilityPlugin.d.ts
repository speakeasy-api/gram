import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DownloadObservabilityPluginRequest, DownloadObservabilityPluginResponse, DownloadObservabilityPluginSecurity } from "../models/operations/downloadobservabilityplugin.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * downloadObservabilityPlugin plugins
 *
 * @remarks
 * Download a ZIP of the per-org observability plugin (Gram hooks). Mints a fresh hooks-scoped API key on each download and embeds it in the plugin's hook script.
 */
export declare function pluginsDownloadObservabilityPlugin(client: GramCore, request: DownloadObservabilityPluginRequest, security?: DownloadObservabilityPluginSecurity | undefined, options?: RequestOptions): APIPromise<Result<DownloadObservabilityPluginResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=pluginsDownloadObservabilityPlugin.d.ts.map