import { Ban } from "lucide-react";
import { Link } from "react-router";
import { useRoutes } from "@/routes";
import { useOrganization } from "@/contexts/Auth";
import { Text } from "@/components/ui/Text";
import { RoleLink } from "@/components/role-link";
import { cn } from "@/lib/utils";
import type { AccessibleMCPServer } from "@gram/client/models/components/accessiblemcpserver.js";
import type { ExplainResourceAccessResult } from "@gram/client/models/components/explainresourceaccessresult.js";
import type { ExplainedAccessRule } from "@gram/client/models/components/explainedaccessrule.js";
import { levelStatus } from "../mcp/access/explainAccess";
import { narrowingLabel } from "../mcp/access/serverAudience";
import { AssignmentServerIcon } from "./AssignmentServerIcon";

export type PersonServerAccess = {
  server: AccessibleMCPServer;
  explanation: ExplainResourceAccessResult;
};

function uniqueSources(
  rules: ExplainedAccessRule[],
  effects: ExplainedAccessRule["effect"][],
): ExplainedAccessRule[] {
  return [
    ...new Map(
      rules
        .filter((rule) => effects.includes(rule.effect))
        .map((rule) => [rule.principalUrn, rule]),
    ).values(),
  ];
}

function RuleSources({ rules }: { rules: ExplainedAccessRule[] }): JSX.Element {
  return (
    <>
      {rules.map((rule, index) => (
        <span key={rule.principalUrn}>
          {index > 0 && ", "}
          {rule.kind === "role" ? (
            <RoleLink principalUrn={rule.principalUrn}>
              {rule.displayName}
            </RoleLink>
          ) : rule.kind === "user" ? (
            "Direct assignment"
          ) : (
            rule.displayName
          )}
        </span>
      ))}
    </>
  );
}

/** Use the server's resolved decision, including grants suppressed by a block. */
export function ServerAssignment({
  server,
  explanation,
}: PersonServerAccess): JSX.Element {
  const organization = useOrganization();
  const routes = useRoutes({ projectSlug: server.projectSlug });
  const project = organization.projects?.find(
    (project) => project.id === server.projectId,
  );
  const level = explanation.levels.find((level) => level.level === "use");
  if (!level)
    return (
      <Text small muted>
        Connection access unavailable.
      </Text>
    );
  const rules = level.rules;
  const publicRoleBlock =
    explanation.visibility === "public" &&
    rules.some((rule) => rule.effect === "blocks");
  const status = publicRoleBlock
    ? { tone: "blocked", label: "Blocked by role" }
    : levelStatus(level, explanation.visibility);
  const blocked = status.tone === "blocked";
  const sources = uniqueSources(rules, ["allows", "overrides", "blocked"]);
  const blockers = uniqueSources(rules, ["blocks"]);
  const limits = uniqueSources(rules, ["limits"]);
  const overridden = uniqueSources(rules, ["overridden"]);
  const restrictions = [
    ...new Set(
      rules
        .filter(
          (rule) =>
            rule.effect !== "blocked" &&
            ((rule.tools?.length ?? 0) > 0 ||
              (rule.dispositions?.length ?? 0) > 0),
        )
        .map((rule) => narrowingLabel(rule)),
    ),
  ];
  return (
    <div
      className={cn(
        "border-border flex items-start gap-3 border-b py-4",
        blocked && "bg-destructive/5",
      )}
    >
      <span className="relative mt-0.5 shrink-0">
        <AssignmentServerIcon server={server} />
        {blocked && (
          <Ban
            aria-hidden="true"
            className="bg-card text-destructive absolute -right-0.5 -bottom-0.5 size-3.5 rounded-full"
          />
        )}
      </span>
      <div className="min-w-0 flex-1 space-y-1.5">
        <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
          <Link
            className={cn(
              "break-words text-sm font-medium hover:underline",
              blocked && "text-destructive",
            )}
            to={routes.mcp.x.overview.href(server.slug || server.id)}
          >
            {server.name || "Unnamed server"}
          </Link>
          <Text small destructive={blocked} muted={!blocked}>
            {status.label}
          </Text>
        </div>
        {server.projectSlug !== "default" && (
          <Text small muted>
            {project?.name || server.projectSlug}
          </Text>
        )}
        <p className="text-muted-foreground text-sm">
          {sources.length > 0 && (
            <>
              Assigned through <RuleSources rules={sources} />
            </>
          )}
          {blockers.length > 0 && (
            <span className="text-destructive">
              {sources.length > 0 ? ", but blocked by " : "Blocked by "}
              <RuleSources rules={blockers} />
            </span>
          )}
          {limits.length > 0 && (
            <>
              {sources.length > 0 ? "; restricted by " : "Restricted by "}
              <RuleSources rules={limits} />
            </>
          )}
          {explanation.visibility === "public" &&
            sources.length === 0 &&
            "Public server"}
        </p>
        {publicRoleBlock && (
          <Text small muted>
            The public endpoint remains accessible without authentication.
          </Text>
        )}
        {overridden.length > 0 && !blocked && (
          <Text small muted>
            Direct assignment overrides the block from{" "}
            <RuleSources rules={overridden} />.
          </Text>
        )}
        {restrictions.length > 0 && !blocked && (
          <Text small muted>
            {restrictions.join(" · ")}
          </Text>
        )}
      </div>
    </div>
  );
}
