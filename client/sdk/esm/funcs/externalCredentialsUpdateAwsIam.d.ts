import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AwsIamCredential } from "../models/components/awsiamcredential.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateAwsIamCredentialRequest, UpdateAwsIamCredentialSecurity } from "../models/operations/updateawsiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateAwsIamCredential externalCredentials
 *
 * @remarks
 * Replace an AWS IAM external credential's configuration. Requires org:admin.
 */
export declare function externalCredentialsUpdateAwsIam(client: GramCore, request: UpdateAwsIamCredentialRequest, security?: UpdateAwsIamCredentialSecurity | undefined, options?: RequestOptions): APIPromise<Result<AwsIamCredential, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsUpdateAwsIam.d.ts.map