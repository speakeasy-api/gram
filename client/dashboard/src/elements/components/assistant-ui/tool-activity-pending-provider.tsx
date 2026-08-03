import {
  useCallback,
  useMemo,
  useState,
  type FC,
  type PropsWithChildren,
} from "react";

import { ToolActivityPendingContext } from "@/elements/hooks/useToolActivityPending";

/**
 * Counts the tool groups in a thread that are still working, so the
 * thread-level thinking indicator can stand down while a group carries the
 * state itself. A count, not a boolean, because a turn can hold several groups.
 */
export const ToolActivityPendingProvider: FC<PropsWithChildren> = ({
  children,
}) => {
  const [count, setCount] = useState(0);

  const register = useCallback(() => {
    setCount((n) => n + 1);
    return () => setCount((n) => Math.max(0, n - 1));
  }, []);

  const value = useMemo(
    () => ({ anyPending: count > 0, register }),
    [count, register],
  );

  return (
    <ToolActivityPendingContext.Provider value={value}>
      {children}
    </ToolActivityPendingContext.Provider>
  );
};
