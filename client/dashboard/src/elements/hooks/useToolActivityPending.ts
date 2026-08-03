import { createContext, useContext, useEffect } from "react";

export interface ToolActivityPendingValue {
  /** True while any tool group in the thread is still working or settling. */
  anyPending: boolean;
  /** Registers a working group; returns the deregister callback. */
  register: () => () => void;
}

/**
 * Tracks whether any tool group in the thread is still working — its tools
 * running, or its label still being summarized.
 *
 * The thread-level thinking indicator and a tool group's own spinner describe
 * the same wait. Shown together they read as two things happening at once, so
 * the indicator stands down while a group is carrying the state itself.
 *
 * The provider lives in its own module (ToolActivityPendingProvider) because a
 * file that exports both a component and hooks breaks fast refresh.
 */
export const ToolActivityPendingContext =
  createContext<ToolActivityPendingValue>({
    anyPending: false,
    register: () => () => {},
  });

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
