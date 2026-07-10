import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListExternalCredentialsResult } from "../models/components/listexternalcredentialsresult.js";
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
  ListGcpIamCredentialsRequest,
  ListGcpIamCredentialsSecurity,
} from "../models/operations/listgcpiamcredentials.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listGcpIamCredentials externalCredentials
 *
 * @remarks
 * List the organization's GCP IAM external credentials. Requires org:read.
 */
export declare function externalCredentialsListGcpIam(
  client: GramCore,
  request?: ListGcpIamCredentialsRequest | undefined,
  security?: ListGcpIamCredentialsSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    ListExternalCredentialsResult,
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
//# sourceMappingURL=externalCredentialsListGcpIam.d.ts.map
