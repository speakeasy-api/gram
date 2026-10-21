import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { MCP_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import { ChevronRight } from "lucide-react";
import { useEffect, useState, type JSX } from "react";
import { Link } from "react-router";
import { WizardStepHeader } from "./WizardChrome";
import type { InventoryServer } from "./useServerInventory";

/**
 * Which servers the agent may reach, and how much of each. A row is the whole
 * decision: the server, the path its runtime will call, whether a person has
 * to authorize it, and the tools the key may use on it.
 */

/** Undefined tools means every tool the policy already allows, now and later. */
export interface ServerSelection {
  server: InventoryServer;
  tools?: string[];
}

export function StepServers({
  servers,
  isLoading,
  isError,
  onRetry,
  selected,
  onChange,
}: {
  servers: InventoryServer[];
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  selected: ServerSelection[];
  onChange: (next: ServerSelection[]) => void;
}): JSX.Element {
  const routes = useRoutes();
  // "Change identity" opens the server's settings in another tab, so the
  // inventory this list was built from is stale the moment it is used. Coming
  // back to this tab is the signal that something may have changed.
  useEffect(() => {
    const refresh = () => {
      if (document.visibilityState === "visible") onRetry();
    };
    window.addEventListener("visibilitychange", refresh);
    return () => window.removeEventListener("visibilitychange", refresh);
  }, [onRetry]);
  const [search, setSearch] = useState("");
  const [showSelectedOnly, setShowSelectedOnly] = useState(false);
  const [showBlocked, setShowBlocked] = useState(false);

  const query = search.trim().toLowerCase();
  const isSelected = (id: string) =>
    selected.some((entry) => entry.server.id === id);
  // A server that authorizes each person separately needs an account attached
  // to the agent before a key can call it, and this flow has no step for that.
  // Offering it would issue a key that reaches the gateway and then fails on
  // every call to that server, so it is shown and not selectable; the agent's
  // own credentials section connects the account.
  const needsAccount = (server: InventoryServer) => Boolean(server.issuerId);
  const matches = servers.filter((server) => {
    if (showSelectedOnly && !isSelected(server.id)) return false;
    if (!query) return true;
    return `${server.name} ${server.slug} ${server.projectName}`
      .toLowerCase()
      .includes(query);
  });
  // Two lists, not one: a per-user server cannot be chosen here at all, and
  // when most of the organization authorizes that way they crowd out the rows
  // that can. They stay reachable — a reader looking for one of them needs to
  // find it and be told why, not find nothing.
  const shown = matches.filter((server) => !needsAccount(server));
  const blockedServers = matches.filter(needsAccount);

  const toggle = (server: InventoryServer) => {
    onChange(
      isSelected(server.id)
        ? selected.filter((entry) => entry.server.id !== server.id)
        : [...selected, { server }],
    );
  };

  return (
    <div className="space-y-5">
      <WizardStepHeader
        title="Server selection"
        description="The MCP servers this agent may call. It can reach nothing else, and never more than you can reach yourself."
      />
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <div className="flex-1">
          <Input
            aria-label="Search servers"
            placeholder={`Search ${servers.length} servers by name or slug`}
            value={search}
            onChange={setSearch}
          />
        </div>
        {/* The design's Filters drawer, reduced to the filter that earns its
            place while the list is one project deep: what is already chosen. */}
        <Button
          variant={showSelectedOnly ? "primary" : "secondary"}
          onClick={() => setShowSelectedOnly((value) => !value)}
          disabled={selected.length === 0 && !showSelectedOnly}
        >
          Selected only · {selected.length}
        </Button>
      </div>

      {isError ? (
        <div role="alert" className="space-y-3">
          <Text>Could not load MCP servers.</Text>
          <Button variant="secondary" onClick={onRetry}>
            Try again
          </Button>
        </div>
      ) : isLoading ? (
        <Text muted>Loading MCP servers…</Text>
      ) : (
        <div className="border-border border">
          <div className="border-border bg-muted/30 flex items-center justify-between gap-3 border-b px-4 py-2">
            <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
              {servers.length} servers · {selected.length} selected
            </span>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="secondary"
                disabled={shown.every((server) => isSelected(server.id))}
                onClick={() => {
                  const additions = shown
                    .filter((server) => !isSelected(server.id))
                    .map((server) => ({ server }));
                  onChange([...selected, ...additions]);
                }}
              >
                Select shown
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={shown.every((server) => !isSelected(server.id))}
                onClick={() => {
                  const hidden = new Set(shown.map((server) => server.id));
                  onChange(
                    selected.filter((entry) => !hidden.has(entry.server.id)),
                  );
                }}
              >
                Clear shown
              </Button>
            </div>
          </div>
          {shown.length === 0 ? (
            <Text muted small className="block px-4 py-10 text-center">
              {servers.length === 0
                ? "No MCP server in this organization can be reached with an API key yet."
                : blockedServers.length > 0
                  ? "Every server matching these filters authorizes each person separately."
                  : "No servers match the current filters."}
            </Text>
          ) : (
            <ul className="divide-border divide-y">
              {shown.map((server) => {
                const checked = isSelected(server.id);
                return (
                  <li key={server.id}>
                    <label
                      className={cn(
                        "flex cursor-pointer items-center gap-4 px-4 py-3",
                        checked ? "bg-accent/40" : "hover:bg-muted/40",
                      )}
                    >
                      <Checkbox
                        checked={checked}
                        onCheckedChange={() => toggle(server)}
                        aria-label={server.name}
                      />
                      <span className="min-w-0 flex-1">
                        <span className="flex flex-wrap items-center gap-2">
                          <span className="truncate">{server.name}</span>
                          <Badge variant="neutral" size="sm">
                            {server.projectName || server.projectSlug}
                          </Badge>
                        </span>
                        <span className="text-muted-foreground block truncate font-mono text-xs">
                          /mcp/{server.slug}
                        </span>
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
          )}
          {blockedServers.length > 0 && (
            <div className="border-border border-t">
              <button
                type="button"
                aria-expanded={showBlocked}
                onClick={() => setShowBlocked((open) => !open)}
                className="hover:bg-muted/40 flex w-full items-center gap-2 px-4 py-3 text-left"
              >
                <ChevronRight
                  aria-hidden="true"
                  className={cn(
                    "size-4 shrink-0 transition-transform",
                    showBlocked && "rotate-90",
                  )}
                />
                <span className="flex-1 text-sm">
                  Unavailable servers (need connected service account)
                </span>
                <Badge variant="neutral" size="sm">
                  {blockedServers.length}
                </Badge>
              </button>
              {showBlocked && (
                <div className="border-border border-t">
                  {/* Said once for the group rather than on every row, and it
                      names both ways out: connect the account afterwards, or
                      stop the server asking each person in the first place. */}
                  <Text muted small className="block px-4 pt-3">
                    These authorize each person separately, so an agent cannot
                    use them until an account is connected from the agent's
                    credentials section. A server that should answer to one
                    shared credential instead can be changed in its settings.
                  </Text>
                  <ul className="divide-border divide-y">
                    {blockedServers.map((server) => (
                      <li
                        key={server.id}
                        className="flex items-center gap-4 px-4 py-3"
                      >
                        <span className="min-w-0 flex-1">
                          <span className="flex flex-wrap items-center gap-2">
                            <span className="truncate">{server.name}</span>
                            <Badge variant="neutral" size="sm">
                              {server.projectName || server.projectSlug}
                            </Badge>
                          </span>
                          <span className="text-muted-foreground block truncate font-mono text-xs">
                            /mcp/{server.slug}
                          </span>
                        </span>
                        {/* A new tab, because this is a detour: the wizard
                            holds the name, scope and selections in memory, and
                            navigating away then coming back loses all of it. */}
                        <Button size="sm" variant="secondary" asChild>
                          <Link
                            to={`${routes.mcp.x.settings.href(
                              server.slug || server.id,
                            )}#${MCP_AUTHENTICATION_SECTION_ID}`}
                            target="_blank"
                            rel="noreferrer"
                          >
                            Change identity
                          </Link>
                        </Button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
