import { MutationKey, UseMutationResult } from "@tanstack/react-query";
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
import { MutationHookOptions } from "./_types.js";
export type CreateSignedChatAttachmentURLMutationVariables = {
  request: CreateSignedChatAttachmentURLRequest;
  security?: CreateSignedChatAttachmentURLSecurity | undefined;
  options?: RequestOptions;
};
export type CreateSignedChatAttachmentURLMutationData =
  CreateSignedChatAttachmentURLResult;
export type CreateSignedChatAttachmentURLMutationError =
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
 * createSignedChatAttachmentURL assets
 *
 * @remarks
 * Create a time-limited signed URL to access a chat attachment without authentication.
 */
export declare function useCreateSignedChatAttachmentURLMutation(
  options?: MutationHookOptions<
    CreateSignedChatAttachmentURLMutationData,
    CreateSignedChatAttachmentURLMutationError,
    CreateSignedChatAttachmentURLMutationVariables
  >,
): UseMutationResult<
  CreateSignedChatAttachmentURLMutationData,
  CreateSignedChatAttachmentURLMutationError,
  CreateSignedChatAttachmentURLMutationVariables
>;
export declare function mutationKeyCreateSignedChatAttachmentURL(): MutationKey;
export declare function buildCreateSignedChatAttachmentURLMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: CreateSignedChatAttachmentURLMutationVariables,
  ) => Promise<CreateSignedChatAttachmentURLMutationData>;
};
//# sourceMappingURL=createSignedChatAttachmentURL.d.ts.map
