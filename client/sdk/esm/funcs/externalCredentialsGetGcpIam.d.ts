import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GcpIamCredential } from "../models/components/gcpiamcredential.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetGcpIamCredentialRequest, GetGcpIamCredentialSecurity } from "../models/operations/getgcpiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getGcpIamCredential externalCredentials
 *
 * @remarks
 * Get a GCP IAM external credential by ID. Requires org:read.
 */
export declare function externalCredentialsGetGcpIam(client: GramCore, request: GetGcpIamCredentialRequest, security?: GetGcpIamCredentialSecurity | undefined, options?: RequestOptions): APIPromise<Result<GcpIamCredential, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsGetGcpIam.d.ts.map