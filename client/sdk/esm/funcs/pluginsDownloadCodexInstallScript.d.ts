import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DownloadCodexInstallScriptRequest, DownloadCodexInstallScriptResponse, DownloadCodexInstallScriptSecurity } from "../models/operations/downloadcodexinstallscript.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * downloadCodexInstallScript plugins
 *
 * @remarks
 * Download a bash install script that registers the Codex observability marketplace and pre-approves all hook events. Requires a published marketplace.
 */
export declare function pluginsDownloadCodexInstallScript(client: GramCore, request?: DownloadCodexInstallScriptRequest | undefined, security?: DownloadCodexInstallScriptSecurity | undefined, options?: RequestOptions): APIPromise<Result<DownloadCodexInstallScriptResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=pluginsDownloadCodexInstallScript.d.ts.map