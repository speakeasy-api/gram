import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AwsIamCredential } from "../models/components/awsiamcredential.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateAwsIamCredentialRequest, CreateAwsIamCredentialSecurity } from "../models/operations/createawsiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createAwsIamCredential externalCredentials
 *
 * @remarks
 * Create an AWS IAM external credential. Requires org:admin.
 */
export declare function externalCredentialsCreateAwsIam(client: GramCore, request: CreateAwsIamCredentialRequest, security?: CreateAwsIamCredentialSecurity | undefined, options?: RequestOptions): APIPromise<Result<AwsIamCredential, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsCreateAwsIam.d.ts.map