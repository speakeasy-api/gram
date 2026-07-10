import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadOpenAPIv3Result } from "../models/components/uploadopenapiv3result.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { FetchOpenAPIv3FromURLRequest, FetchOpenAPIv3FromURLSecurity } from "../models/operations/fetchopenapiv3fromurl.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * fetchOpenAPIv3FromURL assets
 *
 * @remarks
 * Fetch an OpenAPI v3 document from a URL and upload it to Gram.
 */
export declare function assetsFetchOpenAPIv3FromURL(client: GramCore, request: FetchOpenAPIv3FromURLRequest, security?: FetchOpenAPIv3FromURLSecurity | undefined, options?: RequestOptions): APIPromise<Result<UploadOpenAPIv3Result, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assetsFetchOpenAPIv3FromURL.d.ts.map