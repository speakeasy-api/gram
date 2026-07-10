import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GcpIamCredential } from "../models/components/gcpiamcredential.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { CreateGcpIamCredentialRequest, CreateGcpIamCredentialSecurity } from "../models/operations/creategcpiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createGcpIamCredential externalCredentials
 *
 * @remarks
 * Create a GCP IAM external credential. Requires org:admin.
 */
export declare function externalCredentialsCreateGcpIam(client: GramCore, request: CreateGcpIamCredentialRequest, security?: CreateGcpIamCredentialSecurity | undefined, options?: RequestOptions): APIPromise<Result<GcpIamCredential, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsCreateGcpIam.d.ts.map