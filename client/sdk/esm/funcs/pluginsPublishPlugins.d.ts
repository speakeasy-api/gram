import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { PublishPluginsResult } from "../models/components/publishpluginsresult.js";
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
  PublishPluginsRequest,
  PublishPluginsSecurity,
} from "../models/operations/publishplugins.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * publishPlugins plugins
 *
 * @remarks
 * Generate and publish all plugin packages to a GitHub repository.
 */
export declare function pluginsPublishPlugins(
  client: GramCore,
  request: PublishPluginsRequest,
  security?: PublishPluginsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    PublishPluginsResult,
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
//# sourceMappingURL=pluginsPublishPlugins.d.ts.map
