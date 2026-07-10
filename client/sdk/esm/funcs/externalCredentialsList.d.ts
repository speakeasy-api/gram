import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListExternalCredentialsResult } from "../models/components/listexternalcredentialsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListExternalCredentialsRequest, ListExternalCredentialsSecurity } from "../models/operations/listexternalcredentials.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listExternalCredentials externalCredentials
 *
 * @remarks
 * List the organization's external credentials (provider-independent summary). Optionally filter by provider. Requires org:read.
 */
export declare function externalCredentialsList(client: GramCore, request?: ListExternalCredentialsRequest | undefined, security?: ListExternalCredentialsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListExternalCredentialsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsList.d.ts.map