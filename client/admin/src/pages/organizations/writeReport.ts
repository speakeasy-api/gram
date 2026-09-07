import { createContext, useContext } from "react";

/**
 * How a write on an organization reports itself.
 *
 * The list owns both surfaces a write can report on, the live region and the
 * failure banner, and the row controls are drawn inside table cells the list
 * cannot pass props to, so they reach them through context the way the peek
 * controls do. The record reports the same way: its layout provides the
 * reporter, and `Overview` renders through an `<Outlet/>` that takes no prop.
 *
 * Its own module rather than beside `WriteReportProvider`, because a file that
 * exports a component may export nothing else without losing fast refresh.
 */
export type WriteReporter = {
  // Speaks. Every write ends in one of these, whether it succeeded or not.
  announce: (text: string) => void;
  // Shows, and only for a failure with no dialog of its own to report in.
  // `null` clears whatever is showing.
  showFailure: (text: string | null) => void;
};

// A no-op rather than a throw: the peek panel renders these actions too and it
// is mounted on its own in tests.
const NO_REPORTER: WriteReporter = {
  announce: () => {},
  showFailure: () => {},
};

export const WriteReportContext = createContext<WriteReporter>(NO_REPORTER);

export function useWriteReport(): WriteReporter {
  return useContext(WriteReportContext);
}
