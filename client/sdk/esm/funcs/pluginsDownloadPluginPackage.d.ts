import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DownloadPluginPackageRequest,
  DownloadPluginPackageResponse,
  DownloadPluginPackageSecurity,
} from "../models/operations/downloadpluginpackage.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * downloadPluginPackage plugins
 *
 * @remarks
 * Download a ZIP of a single plugin package for direct installation.
 */
export declare function pluginsDownloadPluginPackage(
  client: GramCore,
  request: DownloadPluginPackageRequest,
  security?: DownloadPluginPackageSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    DownloadPluginPackageResponse,
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
//# sourceMappingURL=pluginsDownloadPluginPackage.d.ts.map
