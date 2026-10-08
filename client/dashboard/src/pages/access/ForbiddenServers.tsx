import { Button } from "@/components/ui/Button";
import { Ban } from "lucide-react";
import type { JSX } from "react";

import { ServerTile } from "./McpAccessParts";
import { serverHandle, type ServerWithProject } from "./mcpAccessModel";
import { SERVER_ROW_FRAME } from "./McpServerRow";

export interface ForbiddenServer {
  id: string;
  /** Missing when the server is no longer in the organization's inventory. */
  entry: ServerWithProject | undefined;
}

/**
 * Servers this role forbids, kept apart from the ones it grants: a block
 * beats an allow from any role, so it reads as its own decision.
 */
export function ForbiddenServers({
  servers,
  onUnblock,
}: {
  servers: ForbiddenServer[];
  onUnblock: (id: string) => void;
}): JSX.Element {
  return (
    <section
      aria-labelledby="forbidden-servers-heading"
      className="border-destructive-softest bg-card flex flex-col gap-3 border p-4"
    >
      <div className="flex items-center gap-2">
        <Ban className="text-default-destructive h-4 w-4" />
        <h3
          id="forbidden-servers-heading"
          className="text-default-destructive text-sm"
        >
          Forbidden
        </h3>
        <span className="text-default-destructive font-mono text-xs">
          {servers.length}
        </span>
      </div>
      <p className="text-muted-foreground text-sm">
        Members of this role can&rsquo;t connect to these servers, even if
        another role allows it.
      </p>
      {servers.length === 0 ? (
        <p className="text-muted-foreground text-sm">No forbidden servers.</p>
      ) : (
        <div className="flex flex-col gap-1">
          {servers.map(({ id, entry }) => {
            const name = entry?.server.name ?? id;
            return (
              <div key={id} className={SERVER_ROW_FRAME}>
                <span className="flex min-w-0 flex-1 items-center gap-3 py-1">
                  <ServerTile />
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate text-sm font-medium">{name}</span>
                    <span className="text-muted-foreground truncate font-mono text-xs">
                      {entry
                        ? `${entry.projectName} · ${serverHandle(entry.server)}`
                        : "Not in this organization's servers"}
                    </span>
                  </span>
                </span>
                <Button
                  variant="secondary"
                  size="sm"
                  aria-label={`Unblock ${name}`}
                  onClick={() => onUnblock(id)}
                >
                  <Button.Text>Unblock</Button.Text>
                </Button>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}
