import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AwsIamCredential } from "../models/components/awsiamcredential.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetAwsIamCredentialRequest, GetAwsIamCredentialSecurity } from "../models/operations/getawsiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getAwsIamCredential externalCredentials
 *
 * @remarks
 * Get an AWS IAM external credential by ID. Requires org:read.
 */
export declare function externalCredentialsGetAwsIam(client: GramCore, request: GetAwsIamCredentialRequest, security?: GetAwsIamCredentialSecurity | undefined, options?: RequestOptions): APIPromise<Result<AwsIamCredential, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsGetAwsIam.d.ts.map