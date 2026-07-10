import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { IngestHookResult } from "../models/components/ingesthookresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { IngestHookEventRequest, IngestHookEventSecurity } from "../models/operations/ingesthookevent.js";
import { MutationHookOptions } from "./_types.js";
export type IngestHookEventMutationVariables = {
    request: IngestHookEventRequest;
    security?: IngestHookEventSecurity | undefined;
    options?: RequestOptions;
};
export type IngestHookEventMutationData = IngestHookResult;
export type IngestHookEventMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * ingest hooks
 *
 * @remarks
 * Feature-first unified endpoint for hook events from supported coding assistants.
 */
export declare function useIngestHookEventMutation(options?: MutationHookOptions<IngestHookEventMutationData, IngestHookEventMutationError, IngestHookEventMutationVariables>): UseMutationResult<IngestHookEventMutationData, IngestHookEventMutationError, IngestHookEventMutationVariables>;
export declare function mutationKeyIngestHookEvent(): MutationKey;
export declare function buildIngestHookEventMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: IngestHookEventMutationVariables) => Promise<IngestHookEventMutationData>;
};
//# sourceMappingURL=ingestHookEvent.d.ts.map