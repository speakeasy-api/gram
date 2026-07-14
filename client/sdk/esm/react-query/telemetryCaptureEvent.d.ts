import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CaptureEventResult } from "../models/components/captureeventresult.js";
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
  CaptureEventRequest,
  CaptureEventSecurity,
} from "../models/operations/captureevent.js";
import { MutationHookOptions } from "./_types.js";
export type TelemetryCaptureEventMutationVariables = {
  request: CaptureEventRequest;
  security?: CaptureEventSecurity | undefined;
  options?: RequestOptions;
};
export type TelemetryCaptureEventMutationData = CaptureEventResult;
export type TelemetryCaptureEventMutationError =
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
 * captureEvent telemetry
 *
 * @remarks
 * Capture a telemetry event and forward it to PostHog
 */
export declare function useTelemetryCaptureEventMutation(
  options?: MutationHookOptions<
    TelemetryCaptureEventMutationData,
    TelemetryCaptureEventMutationError,
    TelemetryCaptureEventMutationVariables
  >,
): UseMutationResult<
  TelemetryCaptureEventMutationData,
  TelemetryCaptureEventMutationError,
  TelemetryCaptureEventMutationVariables
>;
export declare function mutationKeyTelemetryCaptureEvent(): MutationKey;
export declare function buildTelemetryCaptureEventMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: TelemetryCaptureEventMutationVariables,
  ) => Promise<TelemetryCaptureEventMutationData>;
};
//# sourceMappingURL=telemetryCaptureEvent.d.ts.map
