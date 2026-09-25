import { useCallback } from "react";
import { useSearchParams } from "react-router";

type UpdateParams = (
  values: Record<string, string | null>,
  replace?: boolean,
) => void;

/** Compose debounced search and clicked controls before React renders the URL. */
export function useFleetParamUpdate(): UpdateParams {
  const [, setParams] = useSearchParams();
  return useCallback(
    (values, replace = false) => {
      // The app's declarative BrowserRouter writes history synchronously.
      // useSearchParams' functional callback receives the last render's params.
      const next = new URLSearchParams(window.location.search);
      for (const [key, value] of Object.entries(values)) {
        if (value === null) next.delete(key);
        else next.set(key, value);
      }
      setParams(next, { replace });
    },
    [setParams],
  );
}
