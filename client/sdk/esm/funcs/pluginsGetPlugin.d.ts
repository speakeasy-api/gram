import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Plugin } from "../models/components/plugin.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  GetPluginRequest,
  GetPluginSecurity,
} from "../models/operations/getplugin.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getPlugin plugins
 *
 * @remarks
 * Get a plugin with its servers and assignments.
 */
export declare function pluginsGetPlugin(
  client: GramCore,
  request: GetPluginRequest,
  security?: GetPluginSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Plugin,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=pluginsGetPlugin.d.ts.map
