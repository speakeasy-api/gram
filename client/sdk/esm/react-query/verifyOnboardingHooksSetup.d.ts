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
  VerifyOnboardingHooksSetupRequest,
  VerifyOnboardingHooksSetupSecurity,
} from "../models/operations/verifyonboardinghookssetup.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildVerifyOnboardingHooksSetupQuery,
  prefetchVerifyOnboardingHooksSetup,
  queryKeyVerifyOnboardingHooksSetup,
  VerifyOnboardingHooksSetupQueryData,
} from "./verifyOnboardingHooksSetup.core.js";
export {
  buildVerifyOnboardingHooksSetupQuery,
  prefetchVerifyOnboardingHooksSetup,
  queryKeyVerifyOnboardingHooksSetup,
  type VerifyOnboardingHooksSetupQueryData,
};
export type VerifyOnboardingHooksSetupQueryError =
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
 * verifyOnboardingHooksSetup organizations
 *
 * @remarks
 * Return recent hook events for the active organization so the onboarding wizard can confirm that Claude Code, Cursor, or Codex instrumentation is delivering events to Gram. Polled from the confirm-traffic step.
 */
export declare function useVerifyOnboardingHooksSetup(
  request?: VerifyOnboardingHooksSetupRequest | undefined,
  security?: VerifyOnboardingHooksSetupSecurity | undefined,
  options?: QueryHookOptions<
    VerifyOnboardingHooksSetupQueryData,
    VerifyOnboardingHooksSetupQueryError
  >,
): UseQueryResult<
  VerifyOnboardingHooksSetupQueryData,
  VerifyOnboardingHooksSetupQueryError
>;
/**
 * verifyOnboardingHooksSetup organizations
 *
 * @remarks
 * Return recent hook events for the active organization so the onboarding wizard can confirm that Claude Code, Cursor, or Codex instrumentation is delivering events to Gram. Polled from the confirm-traffic step.
 */
export declare function useVerifyOnboardingHooksSetupSuspense(
  request?: VerifyOnboardingHooksSetupRequest | undefined,
  security?: VerifyOnboardingHooksSetupSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    VerifyOnboardingHooksSetupQueryData,
    VerifyOnboardingHooksSetupQueryError
  >,
): UseSuspenseQueryResult<
  VerifyOnboardingHooksSetupQueryData,
  VerifyOnboardingHooksSetupQueryError
>;
export declare function setVerifyOnboardingHooksSetupData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      sinceUnixNano?: string | undefined;
      gramSession?: string | undefined;
    },
  ],
  data: VerifyOnboardingHooksSetupQueryData,
): VerifyOnboardingHooksSetupQueryData | undefined;
export declare function invalidateVerifyOnboardingHooksSetup(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        sinceUnixNano?: string | undefined;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllVerifyOnboardingHooksSetup(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=verifyOnboardingHooksSetup.d.ts.map
