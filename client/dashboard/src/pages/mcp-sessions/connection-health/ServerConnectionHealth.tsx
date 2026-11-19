import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useRoutes } from "@/routes";
import { ArrowRight } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router";
import { useProjectProviders } from "../providers";
import {
  connectionHealthNow,
  useSignInSummary,
  useSuspectedProblems,
} from "./api";
import { ConnectionLogsView } from "./ConnectionLogsView";
import { presetWindow, PROBLEM_PARAM, useSetSearchParam } from "./filters";
import { ProblemDetailSheet } from "./ProblemDetailSheet";
import { SuspectedProblems } from "./SuspectedProblems";

const VIEW_PARAM = "view";
const SHEET_PARAMS = [PROBLEM_PARAM];

/** A server's suspected problems, then Active Sessions or its locked Connection Logs. */
export function ServerConnectionHealth({
  providerId,
  server,
  sessions,
}: {
  providerId: string;
  server: { id: string; name: string };
  sessions: ReactNode;
}): JSX.Element {
  const routes = useRoutes();
  const { providers } = useProjectProviders();
  const [searchParams] = useSearchParams();
  const [now] = useState(connectionHealthNow);
  // A registration link opens its sheet, which lives in Active Sessions.
  const view =
    searchParams.get(VIEW_PARAM) === "sessions" ||
    searchParams.has("registration")
      ? "sessions"
      : "logs";
  const scope = useMemo(
    () => ({ providerId, serverId: server.id }),
    [providerId, server.id],
  );
  const problems = useSuspectedProblems(scope);
  // Sample problems show only once logging is known to be on, and only to
  // viewers who read everyone's attempts.
  const window = useMemo(() => presetWindow("1h", now), [now]);
  const summary = useSignInSummary(scope, window, "hour").data;
  const showProblems =
    summary?.loggingEnabled === true && summary.viewerScope === "all";
  const provider = providers.find((p) => p.id === providerId);

  const setParam = useSetSearchParam(SHEET_PARAMS);

  return (
    <div className="space-y-8">
      <SuspectedProblems
        problems={showProblems ? (problems.data?.problems ?? []) : []}
        now={now}
        onOpen={(problem) => setParam(PROBLEM_PARAM, problem.id)}
      />
      <div className="space-y-4">
        <div className="flex items-center justify-between gap-4">
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
          {provider ? (
            <Link
              to={routes.mcpSessions.provider.overview.href(providerId)}
              className="inline-flex items-center gap-1 text-sm hover:underline"
            >
              Provider: {provider.slug}
              <ArrowRight className="size-3.5" aria-hidden />
            </Link>
          ) : null}
        </div>
        {view === "logs" ? (
          <ConnectionLogsView providerId={providerId} lockedServer={server} />
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
