import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateSignedChatAttachmentURLResult } from "../models/components/createsignedchatattachmenturlresult.js";
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
  CreateSignedChatAttachmentURLRequest,
  CreateSignedChatAttachmentURLSecurity,
} from "../models/operations/createsignedchatattachmenturl.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * createSignedChatAttachmentURL assets
 *
 * @remarks
 * Create a time-limited signed URL to access a chat attachment without authentication.
 */
export declare function assetsCreateSignedChatAttachmentURL(
  client: GramCore,
  request: CreateSignedChatAttachmentURLRequest,
  security?: CreateSignedChatAttachmentURLSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    CreateSignedChatAttachmentURLResult,
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
//# sourceMappingURL=assetsCreateSignedChatAttachmentURL.d.ts.map
