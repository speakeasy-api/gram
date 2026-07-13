import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  DeleteGcpIamCredentialRequest,
  DeleteGcpIamCredentialSecurity,
} from "../models/operations/deletegcpiamcredential.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteGcpIamCredential externalCredentials
 *
 * @remarks
 * Soft-delete a GCP IAM external credential by ID. Requires org:admin.
 */
export declare function externalCredentialsDeleteGcpIam(
  client: GramCore,
  request: DeleteGcpIamCredentialRequest,
  security?: DeleteGcpIamCredentialSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=externalCredentialsDeleteGcpIam.d.ts.map
