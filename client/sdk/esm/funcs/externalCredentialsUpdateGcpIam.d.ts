import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GcpIamCredential } from "../models/components/gcpiamcredential.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UpdateGcpIamCredentialRequest, UpdateGcpIamCredentialSecurity } from "../models/operations/updategcpiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * updateGcpIamCredential externalCredentials
 *
 * @remarks
 * Replace a GCP IAM external credential's configuration. Requires org:admin.
 */
export declare function externalCredentialsUpdateGcpIam(client: GramCore, request: UpdateGcpIamCredentialRequest, security?: UpdateGcpIamCredentialSecurity | undefined, options?: RequestOptions): APIPromise<Result<GcpIamCredential, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=externalCredentialsUpdateGcpIam.d.ts.map