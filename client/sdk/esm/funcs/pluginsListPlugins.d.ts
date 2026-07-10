import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListPluginsResult } from "../models/components/listpluginsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListPluginsRequest, ListPluginsSecurity } from "../models/operations/listplugins.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listPlugins plugins
 *
 * @remarks
 * List all plugins for the current project.
 */
export declare function pluginsListPlugins(client: GramCore, request?: ListPluginsRequest | undefined, security?: ListPluginsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListPluginsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=pluginsListPlugins.d.ts.map