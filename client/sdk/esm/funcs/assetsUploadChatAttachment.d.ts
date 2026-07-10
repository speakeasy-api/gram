import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadChatAttachmentResult } from "../models/components/uploadchatattachmentresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { UploadChatAttachmentRequest, UploadChatAttachmentSecurity } from "../models/operations/uploadchatattachment.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * uploadChatAttachment assets
 *
 * @remarks
 * Upload a chat attachment to Gram.
 */
export declare function assetsUploadChatAttachment(client: GramCore, request: UploadChatAttachmentRequest, security?: UploadChatAttachmentSecurity | undefined, options?: RequestOptions): APIPromise<Result<UploadChatAttachmentResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=assetsUploadChatAttachment.d.ts.map