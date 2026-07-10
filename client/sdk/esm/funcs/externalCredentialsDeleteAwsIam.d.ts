import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeleteAwsIamCredentialRequest, DeleteAwsIamCredentialSecurity } from "../models/operations/deleteawsiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteAwsIamCredential externalCredentials
 *
 * @remarks
 * Soft-delete an AWS IAM external credential by ID. Requires org:admin.
 */
export declare function externalCredentialsDeleteAwsIam(client: GramCore, request: DeleteAwsIamCredentialRequest, security?: DeleteAwsIamCredentialSecurity | undefined, options?: RequestOptions): APIPromise<Result<void, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsDeleteAwsIam.d.ts.map