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
  GetAwsIamCredentialRequest,
  GetAwsIamCredentialSecurity,
} from "../models/operations/getawsiamcredential.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetAwsIamCredentialQuery,
  GetAwsIamCredentialQueryData,
  prefetchGetAwsIamCredential,
  queryKeyGetAwsIamCredential,
} from "./getAwsIamCredential.core.js";
export {
  buildGetAwsIamCredentialQuery,
  type GetAwsIamCredentialQueryData,
  prefetchGetAwsIamCredential,
  queryKeyGetAwsIamCredential,
};
export type GetAwsIamCredentialQueryError =
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
 * getAwsIamCredential externalCredentials
 *
 * @remarks
 * Get an AWS IAM external credential by ID. Requires org:read.
 */
export declare function useGetAwsIamCredential(
  request: GetAwsIamCredentialRequest,
  security?: GetAwsIamCredentialSecurity | undefined,
  options?: QueryHookOptions<
    GetAwsIamCredentialQueryData,
    GetAwsIamCredentialQueryError
  >,
): UseQueryResult<GetAwsIamCredentialQueryData, GetAwsIamCredentialQueryError>;
/**
 * getAwsIamCredential externalCredentials
 *
 * @remarks
 * Get an AWS IAM external credential by ID. Requires org:read.
 */
export declare function useGetAwsIamCredentialSuspense(
  request: GetAwsIamCredentialRequest,
  security?: GetAwsIamCredentialSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetAwsIamCredentialQueryData,
    GetAwsIamCredentialQueryError
  >,
): UseSuspenseQueryResult<
  GetAwsIamCredentialQueryData,
  GetAwsIamCredentialQueryError
>;
export declare function setGetAwsIamCredentialData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
    },
  ],
  data: GetAwsIamCredentialQueryData,
): GetAwsIamCredentialQueryData | undefined;
export declare function invalidateGetAwsIamCredential(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetAwsIamCredential(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getAwsIamCredential.d.ts.map
