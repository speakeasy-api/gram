import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UploadChatAttachmentResult } from "../models/components/uploadchatattachmentresult.js";
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
  UploadChatAttachmentRequest,
  UploadChatAttachmentSecurity,
} from "../models/operations/uploadchatattachment.js";
import { MutationHookOptions } from "./_types.js";
export type UploadChatAttachmentMutationVariables = {
  request: UploadChatAttachmentRequest;
  security?: UploadChatAttachmentSecurity | undefined;
  options?: RequestOptions;
};
export type UploadChatAttachmentMutationData = UploadChatAttachmentResult;
export type UploadChatAttachmentMutationError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * uploadChatAttachment assets
 *
 * @remarks
 * Upload a chat attachment to Gram.
 */
export declare function useUploadChatAttachmentMutation(
  options?: MutationHookOptions<
    UploadChatAttachmentMutationData,
    UploadChatAttachmentMutationError,
    UploadChatAttachmentMutationVariables
  >,
): UseMutationResult<
  UploadChatAttachmentMutationData,
  UploadChatAttachmentMutationError,
  UploadChatAttachmentMutationVariables
>;
export declare function mutationKeyUploadChatAttachment(): MutationKey;
export declare function buildUploadChatAttachmentMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: UploadChatAttachmentMutationVariables,
  ) => Promise<UploadChatAttachmentMutationData>;
};
//# sourceMappingURL=uploadChatAttachment.d.ts.map
