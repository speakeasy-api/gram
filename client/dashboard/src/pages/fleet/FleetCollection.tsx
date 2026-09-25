import { formatDistanceToNow } from "date-fns";
import { Badge } from "@/components/ui/Badge";
import {
  Bot,
  Building2,
  ChevronRight,
  MessagesSquare,
  Network,
  UserRound,
} from "lucide-react";
import { fleetDirectory, sourceLabels, type FleetRow } from "./fleet-model";

const lifecycleLabels = {
  active: "Registered",
  suspended: "Suspended",
  revoked: "Revoked",
};
const glyphs = { agent: Bot, assistant: Bot, session: MessagesSquare };
export function FleetStatus({
  row,
  blocked,
}: {
  row: FleetRow;
  blocked?: boolean;
}): JSX.Element {
  return (
    <div className="fleet-status">
      {row.agent && <span>{lifecycleLabels[row.agent.lifecycle]}</span>}
      {row.source === "assistant" && (
        <span>
          {row.assistant?.status === "paused"
            ? "Assistant paused"
            : "Configured"}
        </span>
      )}
      {row.source === "session" && <span>Captured</span>}
      {row.session?.riskFindingsCount != null &&
        row.session.riskFindingsCount > 0 && (
          <span className="text-destructive">
            {row.session.riskFindingsCount}{" "}
            {row.session.riskFindingsCount === 1 ? "finding" : "findings"}
          </span>
        )}
      {blocked && (
        <Badge variant="destructive">
          <Badge.Text>MCP blocked</Badge.Text>
        </Badge>
      )}
    </div>
  );
}
export function FleetCollection({
  rows,
  view,
  selected,
  onSelect,
  blocked,
  projectName,
}: {
  rows: FleetRow[];
  view: string;
  selected: string | null;
  onSelect: (id: string) => void;
  blocked: ReadonlySet<string>;
  projectName: string;
}): JSX.Element {
  if (view !== "directory")
    return (
      <div className="fleet-list" aria-label="Fleet source groups">
        {(["agent", "assistant", "session"] as const).map((source) => {
          const group = rows.filter((row) => row.source === source);
          if (!group.length) return null;
          return (
            <section key={source} aria-label={sourceLabels[source]}>
              <h2 className="fleet-source-heading">
                {sourceLabels[source]}
                <span>{group.length} loaded</span>
              </h2>
              {group.map((row) => (
                <div
                  className="fleet-list-row"
                  key={row.id}
                  data-selected={selected === row.id}
                >
                  <div className="fleet-list-identity">
                    <RowTitle
                      row={row}
                      selected={selected}
                      onSelect={onSelect}
                    />
                    <div className="fleet-row-person">
                      {row.personRole}: {row.personName}
                      <span> · {row.department}</span>
                    </div>
                  </div>
                  <div className="fleet-row-signals">
                    <FleetStatus row={row} blocked={blocked.has(row.id)} />
                    <LastActivity row={row} />
                  </div>
                </div>
              ))}
            </section>
          );
        })}
        {!rows.length && (
          <p className="p-4 text-sm">
            No observed activity in the last 24 hours matches these filters.
          </p>
        )}
      </div>
    );
  const departments = fleetDirectory(rows);
  return (
    <div
      className="fleet-directory"
      aria-label="Department and identity directory"
    >
      <div className="fleet-directory-root">
        <Network size={18} />
        <strong>{projectName}</strong>
        <span className="text-muted-foreground text-xs">
          Directory attribution
        </span>
      </div>
      {departments.map((department) => (
        <section className="fleet-department" key={department.name}>
          <h2>
            <Building2 size={16} />
            {department.name}
          </h2>
          {department.identities.map((identity) => (
            <div className="fleet-person" key={identity.id}>
              <h3>
                <UserRound size={16} />
                <span>{identity.name}</span>
                <small>{identity.roles.join(" · ")}</small>
              </h3>
              <div className="fleet-branches">
                {identity.rows.map((row) => (
                  <div className="fleet-branch" key={row.id}>
                    <RowTitle
                      row={row}
                      selected={selected}
                      onSelect={onSelect}
                    />
                    <FleetStatus row={row} blocked={blocked.has(row.id)} />
                    <ChevronRight size={14} aria-hidden />
                  </div>
                ))}
              </div>
            </div>
          ))}
        </section>
      ))}
      {!rows.length && (
        <p className="p-4 text-sm">
          No observed activity in the last 24 hours matches these filters.
        </p>
      )}
    </div>
  );
}
function RowTitle({
  row,
  selected,
  onSelect,
}: {
  row: FleetRow;
  selected: string | null;
  onSelect: (id: string) => void;
}): JSX.Element {
  const Glyph = glyphs[row.source];
  return (
    <button
      className="fleet-row-title"
      data-fleet-row={row.id}
      aria-current={selected === row.id ? "true" : undefined}
      onClick={() => onSelect(row.id)}
    >
      <Glyph size={18} aria-hidden />
      <span>
        <strong>{row.title}</strong>
        <small>
          {row.subtitle} · {row.personRole}
        </small>
      </span>
    </button>
  );
}

function LastActivity({ row }: { row: FleetRow }): JSX.Element {
  return row.lastActivity ? (
    <time
      className="text-muted-foreground text-xs"
      dateTime={row.lastActivity.toISOString()}
      title={`${row.lastActivity.toLocaleString()}${row.agent ? " · Credential authenticated, organization-wide" : ""}`}
    >
      {row.agent && "Credential authenticated · "}
      {formatDistanceToNow(row.lastActivity, { addSuffix: true })}
    </time>
  ) : (
    <span className="text-muted-foreground text-xs">
      {row.source === "assistant"
        ? "No captures loaded"
        : "Activity not attributed"}
    </span>
  );
}
