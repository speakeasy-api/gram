import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListVersionsResult } from "../models/components/listversionsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListVersionsRequest, ListVersionsSecurity } from "../models/operations/listversions.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listVersions packages
 *
 * @remarks
 * List published versions of a package.
 */
export declare function packagesListVersions(client: GramCore, request: ListVersionsRequest, security?: ListVersionsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListVersionsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=packagesListVersions.d.ts.map