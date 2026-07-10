import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { TriggerInstance } from "../models/components/triggerinstance.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { PauseTriggerInstanceRequest, PauseTriggerInstanceSecurity } from "../models/operations/pausetriggerinstance.js";
import { MutationHookOptions } from "./_types.js";
export type PauseTriggerMutationVariables = {
    request: PauseTriggerInstanceRequest;
    security?: PauseTriggerInstanceSecurity | undefined;
    options?: RequestOptions;
};
export type PauseTriggerMutationData = TriggerInstance;
export type PauseTriggerMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * pauseTriggerInstance triggers
 *
 * @remarks
 * Pause a trigger instance.
 */
export declare function usePauseTriggerMutation(options?: MutationHookOptions<PauseTriggerMutationData, PauseTriggerMutationError, PauseTriggerMutationVariables>): UseMutationResult<PauseTriggerMutationData, PauseTriggerMutationError, PauseTriggerMutationVariables>;
export declare function mutationKeyPauseTrigger(): MutationKey;
export declare function buildPauseTriggerMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: PauseTriggerMutationVariables) => Promise<PauseTriggerMutationData>;
};
//# sourceMappingURL=pauseTrigger.d.ts.map