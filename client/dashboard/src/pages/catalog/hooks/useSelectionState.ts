import { useCallback, useMemo } from "react";
import { useSearchParams } from "react-router";

/**
 * Hook to manage selected servers with URL synchronization.
 * Selected servers persist across navigation.
 */
export function useSelectionState() {
  const [searchParams, setSearchParams] = useSearchParams();

  // Parse selected servers from URL
  const selectedServers = useMemo<Set<string>>(() => {
    const selectedParam = searchParams.get("selected");
    if (!selectedParam) return new Set();
    return new Set(selectedParam.split(",").filter(Boolean));
  }, [searchParams]);

  // Toggle a server's selection
  const toggleServerSelection = useCallback(
    (serverKey: string) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          const current = params.get("selected");
          const selected = new Set(
            current ? current.split(",").filter(Boolean) : [],
          );

          if (selected.has(serverKey)) {
            selected.delete(serverKey);
          } else {
            selected.add(serverKey);
          }

          if (selected.size > 0) {
            params.set("selected", Array.from(selected).join(","));
          } else {
            params.delete("selected");
          }

          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Clear all selections
  const clearSelection = useCallback(() => {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        params.delete("selected");
        return params;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  return {
    selectedServers,
    toggleServerSelection,
    clearSelection,
  };
}
