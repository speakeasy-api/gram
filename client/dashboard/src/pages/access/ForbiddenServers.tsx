import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { Ban } from "lucide-react";
import type { JSX } from "react";

import { ServerMark } from "./McpAccessParts";
import { serverHandle, type ServerWithProject } from "./mcpAccessModel";
import { LIST_FRAME, LIST_ROW } from "./mcpAccessStyles";

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
  onRemove,
}: {
  servers: ForbiddenServer[];
  onRemove: (id: string) => void;
}): JSX.Element {
  return (
    <section aria-labelledby="forbidden-servers-heading" className={LIST_FRAME}>
      {/* Title over its explanation, with room around both: the explanation
          runs long, and wrapped beside the title it crowded the row. */}
      <div className="bg-muted/40 flex flex-col gap-1.5 px-3 py-3">
        <span className="flex items-center gap-2">
          <Ban className="text-default-destructive h-3.5 w-3.5" />
          <h3
            id="forbidden-servers-heading"
            className="text-eyebrow text-default-destructive"
          >
            Forbidden ({servers.length})
          </h3>
        </span>
        <Text small muted>
          Members of this role can&rsquo;t connect to these servers, even if
          another role allows it. Only a grant made to a member directly for the
          server overrides this.
        </Text>
      </div>
      {servers.length === 0 ? (
        <Text muted small className="px-3 py-2">
          No forbidden servers.
        </Text>
      ) : (
        servers.map(({ id, entry }) => {
          const name = entry?.server.name ?? id;
          return (
            <div key={id} className={LIST_ROW}>
              <span className="flex min-w-0 flex-1 items-center gap-3">
                <ServerMark />
                <span className="flex min-w-0 flex-col">
                  <Text as="span" className="truncate text-sm font-medium">
                    {name}
                  </Text>
                  <Text as="span" mono small muted className="truncate">
                    {entry
                      ? `${entry.projectName} · ${serverHandle(entry.server)}`
                      : "Not in this organization's servers"}
                  </Text>
                </span>
              </span>
              <Button
                variant="secondary"
                size="sm"
                aria-label={`Remove ${name} from Forbidden`}
                onClick={() => onRemove(id)}
              >
                <Button.Text>Remove</Button.Text>
              </Button>
            </div>
          );
        })
      )}
    </section>
  );
}
