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
  ServeChatAttachmentSignedRequest,
  ServeChatAttachmentSignedResponse,
} from "../models/operations/servechatattachmentsigned.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * serveChatAttachmentSigned assets
 *
 * @remarks
 * Serve a chat attachment using a signed URL token.
 */
export declare function assetsServeChatAttachmentSigned(
  client: GramCore,
  request: ServeChatAttachmentSignedRequest,
  options?: RequestOptions,
): APIPromise<
  Result<
    ServeChatAttachmentSignedResponse,
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
//# sourceMappingURL=assetsServeChatAttachmentSigned.d.ts.map
