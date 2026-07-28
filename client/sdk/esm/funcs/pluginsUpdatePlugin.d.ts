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
  UpdatePluginRequest,
  UpdatePluginSecurity,
} from "../models/operations/updateplugin.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updatePlugin plugins
 *
 * @remarks
 * Update plugin metadata.
 */
export declare function pluginsUpdatePlugin(
  client: GramCore,
  request: UpdatePluginRequest,
  security?: UpdatePluginSecurity | undefined,
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
//# sourceMappingURL=pluginsUpdatePlugin.d.ts.map
