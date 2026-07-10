import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PluginServer } from "../models/components/pluginserver.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdatePluginServerRequest, UpdatePluginServerSecurity } from "../models/operations/updatepluginserver.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updatePluginServer plugins
 *
 * @remarks
 * Update a server's configuration within a plugin.
 */
export declare function pluginsUpdatePluginServer(client: GramCore, request: UpdatePluginServerRequest, security?: UpdatePluginServerSecurity | undefined, options?: RequestOptions): APIPromise<Result<PluginServer, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=pluginsUpdatePluginServer.d.ts.map