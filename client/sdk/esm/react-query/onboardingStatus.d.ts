import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
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
  GetOnboardingStatusRequest,
  GetOnboardingStatusSecurity,
} from "../models/operations/getonboardingstatus.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildOnboardingStatusQuery,
  OnboardingStatusQueryData,
  prefetchOnboardingStatus,
  queryKeyOnboardingStatus,
} from "./onboardingStatus.core.js";
export {
  buildOnboardingStatusQuery,
  type OnboardingStatusQueryData,
  prefetchOnboardingStatus,
  queryKeyOnboardingStatus,
};
export type OnboardingStatusQueryError =
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
 * getOnboardingStatus organizations
 *
 * @remarks
 * Get the onboarding status for the active organization by checking WorkOS SSO connections and directory sync state.
 */
export declare function useOnboardingStatus(
  request?: GetOnboardingStatusRequest | undefined,
  security?: GetOnboardingStatusSecurity | undefined,
  options?: QueryHookOptions<
    OnboardingStatusQueryData,
    OnboardingStatusQueryError
  >,
): UseQueryResult<OnboardingStatusQueryData, OnboardingStatusQueryError>;
/**
 * getOnboardingStatus organizations
 *
 * @remarks
 * Get the onboarding status for the active organization by checking WorkOS SSO connections and directory sync state.
 */
export declare function useOnboardingStatusSuspense(
  request?: GetOnboardingStatusRequest | undefined,
  security?: GetOnboardingStatusSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    OnboardingStatusQueryData,
    OnboardingStatusQueryError
  >,
): UseSuspenseQueryResult<
  OnboardingStatusQueryData,
  OnboardingStatusQueryError
>;
export declare function setOnboardingStatusData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: OnboardingStatusQueryData,
): OnboardingStatusQueryData | undefined;
export declare function invalidateOnboardingStatus(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllOnboardingStatus(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=onboardingStatus.d.ts.map
