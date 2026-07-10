import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListExternalCredentialsResult } from "../models/components/listexternalcredentialsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAwsIamCredentialsRequest, ListAwsIamCredentialsSecurity } from "../models/operations/listawsiamcredentials.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listAwsIamCredentials externalCredentials
 *
 * @remarks
 * List the organization's AWS IAM external credentials. Requires org:read.
 */
export declare function externalCredentialsListAwsIam(client: GramCore, request?: ListAwsIamCredentialsRequest | undefined, security?: ListAwsIamCredentialsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListExternalCredentialsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsListAwsIam.d.ts.map