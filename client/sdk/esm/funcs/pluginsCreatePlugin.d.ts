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
  CreatePluginRequest,
  CreatePluginSecurity,
} from "../models/operations/createplugin.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createPlugin plugins
 *
 * @remarks
 * Create a new plugin.
 */
export declare function pluginsCreatePlugin(
  client: GramCore,
  request: CreatePluginRequest,
  security?: CreatePluginSecurity | undefined,
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
//# sourceMappingURL=pluginsCreatePlugin.d.ts.map
