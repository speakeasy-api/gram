import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useRoutes } from "@/routes";
import { useNavigate } from "react-router";
import { ConnectionsTab } from "../ConnectionsTab";
import { ConnectionLogsView } from "./ConnectionLogsView";

export type ConnectionsView = "sessions" | "logs";

const VIEW_OPTIONS: { value: ConnectionsView; label: string }[] = [
  { value: "sessions", label: "Active Sessions" },
  { value: "logs", label: "Connection Logs" },
];

/** The Connections tab with connection health: one of two views at a time. */
export function ConnectionsViews({
  view,
  providerId,
}: {
  view: ConnectionsView;
  providerId: string;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  return (
    <div className="space-y-4">
      <SegmentedControl
        ariaLabel="Connections view"
        value={view}
        options={VIEW_OPTIONS}
        onChange={(next) =>
          void navigate(
            next === "logs"
              ? routes.mcpSessions.provider.connectionLogs.href(providerId)
              : routes.mcpSessions.provider.connections.href(providerId),
          )
        }
      />
      {view === "logs" ? (
        <ConnectionLogsView providerId={providerId} />
      ) : (
        <ConnectionsTab providerId={providerId} />
      )}
    </div>
  );
}
