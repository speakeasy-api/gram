import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListResourcesResult } from "../models/components/listresourcesresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListResourcesRequest, ListResourcesSecurity } from "../models/operations/listresources.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listResources resources
 *
 * @remarks
 * List all resources for a project
 */
export declare function resourcesList(client: GramCore, request?: ListResourcesRequest | undefined, security?: ListResourcesSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListResourcesResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=resourcesList.d.ts.map