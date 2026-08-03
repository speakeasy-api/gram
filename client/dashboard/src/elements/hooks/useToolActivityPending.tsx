import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type FC,
  type PropsWithChildren,
} from "react";

interface ToolActivityPendingValue {
  /** True while any tool group in the thread is still working or settling. */
  anyPending: boolean;
  /** Registers a working group; returns the deregister callback. */
  register: () => () => void;
}

const ToolActivityPendingContext = createContext<ToolActivityPendingValue>({
  anyPending: false,
  register: () => () => {},
});

/**
 * Tracks whether any tool group in the thread is still working — its tools
 * running, or its label still being summarized.
 *
 * The thread-level thinking indicator and a tool group's own spinner describe
 * the same wait. Shown together they read as two things happening at once, so
 * the indicator stands down while a group is carrying the state itself. A
 * count, not a boolean, because a turn can hold several groups.
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

/** True while any tool group in the thread is working or settling. */
export function useAnyToolActivityPending(): boolean {
  return useContext(ToolActivityPendingContext).anyPending;
}

/** Registers this group as working for as long as `pending` holds. */
export function useReportToolActivityPending(pending: boolean): void {
  const { register } = useContext(ToolActivityPendingContext);
  useEffect(() => {
    if (!pending) return;
    return register();
  }, [pending, register]);
}
