import { Type } from "@/components/ui/type";
import { DotCard } from "@/components/ui/dot-card";
import type { PortalServer } from "@gram/client/models/components";

interface Props {
  server: PortalServer;
}

export function PortalCard({ server }: Props) {
  return (
    <a href={server.installUrl} className="block" rel="noopener noreferrer">
      <DotCard>
        <div className="flex flex-col gap-2">
          <Type variant="subheading" as="div">
            {server.name}
          </Type>
          {server.description ? (
            <Type small muted className="line-clamp-2">
              {server.description}
            </Type>
          ) : null}
          <Type small muted>
            {server.toolCount} {server.toolCount === 1 ? "tool" : "tools"}
          </Type>
        </div>
      </DotCard>
    </a>
  );
}
