import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeletePluginRequest, DeletePluginSecurity } from "../models/operations/deleteplugin.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deletePlugin plugins
 *
 * @remarks
 * Delete a plugin.
 */
export declare function pluginsDeletePlugin(client: GramCore, request: DeletePluginRequest, security?: DeletePluginSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=pluginsDeletePlugin.d.ts.map