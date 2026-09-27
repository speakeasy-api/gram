import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { KeyServer } from "./AgentKeyServers";
import { reviewLabel, summarizeKeyReview } from "./agent-key-review";

/** Supply only stored upstream identities from the canonical session view, never dashboard user data. */
export interface KeyReviewAccount {
  resourceId: string;
  projectId: string;
  status: "connected" | "not-connected" | "not-required";
  displayName?: string;
  email?: string;
}

export interface AgentKeyReviewProps {
  name: string;
  servers: KeyServer[];
  grants: AgentPolicyGrantForm[];
  accounts?: KeyReviewAccount[];
  onEditServers: () => void;
  onEditAccounts: () => void;
  onEditAccess: () => void;
  disabled?: boolean;
}

export function AgentKeyReview({
  name,
  servers,
  grants,
  accounts,
  onEditServers,
  onEditAccounts,
  onEditAccess,
  disabled,
}: AgentKeyReviewProps): JSX.Element {
  const review = summarizeKeyReview(servers, grants);
  return (
    <section aria-label="Key review" className="space-y-5">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold">Review your key</h2>
        <Text small muted>
          Confirm where{" "}
          <span className="font-medium text-foreground">{name}</span> can
          connect and what it can do.
        </Text>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-medium">
          {servers.length} MCP {servers.length === 1 ? "server" : "servers"}
        </h3>
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={onEditServers}
          disabled={disabled}
        >
          Edit servers
        </Button>
      </div>
      <div className="divide-y rounded-lg border">
        {review.servers.map(({ server, access }) => {
          const ids = [
            server.id,
            server.resourceId,
            server.projectId,
            server.issuerId ?? "",
          ];
          const serverName =
            reviewLabel(server.name, ids) ?? "Server name unavailable";
          const project = reviewLabel(server.projectSlug, ids);
          const connectedAccounts = accounts?.filter(
            (account) =>
              account.resourceId === server.resourceId &&
              account.projectId === server.projectId,
          );
          return (
            <article
              key={`${server.projectId}:${server.id}`}
              aria-label={serverName}
              className="space-y-4 p-4"
            >
              <div>
                <h4 className="font-semibold">{serverName}</h4>
                {project ? (
                  <Text small muted>
                    {project}
                  </Text>
                ) : null}
              </div>
              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <h5 className="text-sm font-medium">Allowed access</h5>
                  <Button
                    type="button"
                    variant="tertiary"
                    size="sm"
                    aria-label={`Edit access for ${serverName}`}
                    onClick={onEditAccess}
                    disabled={disabled}
                  >
                    Edit access
                  </Button>
                </div>
                {access.length ? (
                  <ul className="space-y-3">
                    {access.map((group, index) => (
                      <li key={index} className="space-y-1 text-sm">
                        <p className="font-medium">
                          {group.action}
                          {group.condition ? ` · ${group.condition}` : ""}
                        </p>
                        {group.allTools || group.categoryTools ? (
                          <p className="text-muted-foreground">
                            {group.condition
                              ? "All eligible tools in this category, including future tools."
                              : "All eligible tools, including future tools."}
                          </p>
                        ) : (
                          <ul
                            className="flex flex-wrap gap-1.5"
                            aria-label="Selected tools"
                          >
                            {group.tools.map((tool) => (
                              <li
                                key={tool}
                                className="max-w-full break-words rounded-md bg-muted px-2 py-1"
                              >
                                {reviewLabel(tool, ids) ??
                                  "Tool name unavailable"}
                              </li>
                            ))}
                          </ul>
                        )}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <Text small muted>
                    {server.kind === "Unproxied"
                      ? "This server does not use a Gram API key."
                      : "No access selected for this server."}
                  </Text>
                )}
              </div>
              <div className="border-t pt-3">
                <div className="flex items-center justify-between gap-2">
                  <h5 className="text-sm font-medium">Connected accounts</h5>
                  <Button
                    type="button"
                    variant="tertiary"
                    size="sm"
                    aria-label={`Edit accounts for ${serverName}`}
                    onClick={onEditAccounts}
                    disabled={disabled}
                  >
                    Edit accounts
                  </Button>
                </div>
                {connectedAccounts?.length ? (
                  <ul className="space-y-1 text-sm text-muted-foreground">
                    {connectedAccounts.map((account, index) => {
                      const identity = [
                        reviewLabel(account.displayName, ids),
                        reviewLabel(account.email, ids),
                      ].filter(Boolean);
                      return (
                        <li key={index}>
                          {account.status === "connected"
                            ? identity.length
                              ? identity.join(" · ")
                              : "Account connected · Identity unavailable"
                            : account.status === "not-required"
                              ? "No connected account required."
                              : "Account not connected"}
                        </li>
                      );
                    })}
                  </ul>
                ) : (
                  <Text small muted>
                    {server.issuerId
                      ? "Account identity unavailable. Review accounts to confirm the connection."
                      : "No connected account required."}
                  </Text>
                )}
              </div>
            </article>
          );
        })}
      </div>
      {review.hasBroadAccess ? (
        <Text small role="alert">
          Some access applies beyond these servers or projects. Edit access to
          limit this key to your selection.
        </Text>
      ) : null}
      {review.hasUnmappedAccess ? (
        <Text small role="alert">
          Some access could not be matched to these servers. Edit access before
          creating this key.
        </Text>
      ) : null}
      {!grants.length ? (
        <Text small role="alert">
          No access selected. Choose allowed tools before creating this key.
        </Text>
      ) : null}
    </section>
  );
}
