import { ServiceError } from "@gram/client/models/errors/serviceerror";

/**
 * Check if logs are enabled based on the API response error.
 * The backend returns a 404 when logs are not enabled for the organization.
 */
function isLogsEnabled(error: Error | null): boolean {
  if (!error) return true;
  if (error instanceof ServiceError && error.statusCode === 404) return false;
  if (
    "statusCode" in error &&
    (error as { statusCode: number }).statusCode === 404
  )
    return false;
  return true;
}

/**
 * Wraps a query result to add `isLogsDisabled` derived from the error state.
 * Use this with any observability query that returns 404 when logs aren't enabled.
 *
 * @example
 * const { data, isLoading, isLogsDisabled } = useLogsEnabledErrorCheck(
 *   useListChatsWithResolutions({ ... })
 * );
 */
export function useLogsEnabledErrorCheck<T extends { error: Error | null }>(
  queryResult: T,
): T & { isLogsDisabled: boolean } {
  return {
    ...queryResult,
    isLogsDisabled: !isLogsEnabled(queryResult.error),
  };
}
