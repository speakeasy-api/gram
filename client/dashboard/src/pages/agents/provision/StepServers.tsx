import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useState, type JSX } from "react";
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
  const [search, setSearch] = useState("");
  const [showSelectedOnly, setShowSelectedOnly] = useState(false);

  const query = search.trim().toLowerCase();
  const isSelected = (id: string) =>
    selected.some((entry) => entry.server.id === id);
  const shown = servers.filter((server) => {
    if (showSelectedOnly && !isSelected(server.id)) return false;
    if (!query) return true;
    return `${server.name} ${server.slug} ${server.projectName}`
      .toLowerCase()
      .includes(query);
  });

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
        description={
          <>
            Grants <code className="text-xs">mcp:connect</code> on each selected
            server. Keys are narrowed again at issuance against the owner's live
            permissions.
          </>
        }
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
                disabled={shown.length === 0}
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
                      {server.issuerId && (
                        <Badge variant="neutral" size="sm">
                          Per-user OAuth
                        </Badge>
                      )}
                    </label>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
