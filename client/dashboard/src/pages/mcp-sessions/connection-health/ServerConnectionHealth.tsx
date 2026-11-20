import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useMemo, useState, type ReactNode } from "react";
import { useSearchParams } from "react-router";
import {
  connectionHealthNow,
  useSignInSummary,
  useSuspectedProblems,
} from "./api";
import { ConnectionLogsView } from "./ConnectionLogsView";
import { presetWindow, PROBLEM_PARAM, useSetSearchParam } from "./filters";
import { SERVICE_DEFINITION } from "./format";
import { ProblemDetailSheet } from "./ProblemDetailSheet";
import { SuspectedProblems } from "./SuspectedProblems";

const VIEW_PARAM = "view";
const SHEET_PARAMS = [PROBLEM_PARAM];

/** A server's suspected problems, then Active Sessions or its locked Connection Logs. */
export function ServerConnectionHealth({
  providerId,
  serverId,
  sessions,
}: {
  providerId: string;
  serverId: string;
  sessions: ReactNode;
}): JSX.Element {
  const [searchParams] = useSearchParams();
  const [now] = useState(connectionHealthNow);
  // A registration link opens its sheet, which lives in Active Sessions.
  const view =
    searchParams.get(VIEW_PARAM) === "sessions" ||
    searchParams.has("registration")
      ? "sessions"
      : "logs";
  const scope = useMemo(
    () => ({ providerId, serverId }),
    [providerId, serverId],
  );
  const problems = useSuspectedProblems(scope);
  // Sample problems show only once logging is known to be on, and only to
  // viewers who read everyone's attempts.
  const window = useMemo(() => presetWindow("1h", now), [now]);
  const summary = useSignInSummary(scope, window, "hour").data;
  const showProblems =
    summary?.loggingEnabled === true && summary.viewerScope === "all";
  const shownProblems = showProblems ? (problems.data?.problems ?? []) : [];

  const setParam = useSetSearchParam(SHEET_PARAMS);

  return (
    <div className="space-y-8">
      <div className="space-y-3 empty:hidden">
        <SuspectedProblems
          problems={shownProblems}
          now={now}
          onOpen={(problem) => setParam(PROBLEM_PARAM, problem.id)}
        />
        {shownProblems.some((p) => p.part === "service") ? (
          <p className="text-muted-foreground text-sm">{SERVICE_DEFINITION}</p>
        ) : null}
      </div>
      <div className="space-y-4">
        <div className="flex">
          <SegmentedControl
            ariaLabel="Connections view"
            variant="view"
            value={view}
            onChange={(next) =>
              setParam(VIEW_PARAM, next === "sessions" ? "sessions" : null)
            }
            options={[
              { value: "sessions", label: "Active Sessions" },
              { value: "logs", label: "Connection Logs" },
            ]}
          />
        </div>
        {view === "logs" ? (
          <ConnectionLogsView providerId={providerId} serverId={serverId} />
        ) : (
          sessions
        )}
      </div>
      <ProblemDetailSheet
        id={showProblems ? searchParams.get(PROBLEM_PARAM) : null}
        providerId={providerId}
        now={now}
        onClose={() => setParam(PROBLEM_PARAM, null)}
      />
    </div>
  );
}
