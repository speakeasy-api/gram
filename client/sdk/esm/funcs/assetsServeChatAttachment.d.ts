import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ServeChatAttachmentRequest, ServeChatAttachmentResponse, ServeChatAttachmentSecurity } from "../models/operations/servechatattachment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * serveChatAttachment assets
 *
 * @remarks
 * Serve a chat attachment from Gram.
 */
export declare function assetsServeChatAttachment(client: GramCore, request: ServeChatAttachmentRequest, security?: ServeChatAttachmentSecurity | undefined, options?: RequestOptions): APIPromise<Result<ServeChatAttachmentResponse, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assetsServeChatAttachment.d.ts.map