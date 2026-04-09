import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { Badge } from "@/components/ui/badge";
import { useOrgRoutes } from "@/routes";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { ArrowRight, Eye, LayoutGrid, Lock, Server } from "lucide-react";
import { Link, useNavigate } from "react-router";
import { useCollectionServers } from "./hooks";
import type { Collection } from "./types";

export function CollectionCard({ collection }: { collection: Collection }) {
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const { servers } = useCollectionServers(collection.slug);
  const serverCount = servers.length;
  const detailHref = orgRoutes.collections.detail.href(collection.slug);

  return (
    <DotCard
      className="cursor-pointer"
      onClick={() => navigate(detailHref)}
      icon={
        <LayoutGrid className="w-10 h-10 text-muted-foreground opacity-60" />
      }
      overlay={
        collection.visibility === "private" ? (
          <div className="absolute top-3.5 left-3.5 z-10">
            <Badge
              variant="outline"
              className="border-muted-foreground/30 bg-background/80 text-muted-foreground backdrop-blur-sm"
            >
              <Lock className="w-3 h-3 mr-1" />
              Private
            </Badge>
          </div>
        ) : undefined
      }
    >
      <div className="flex items-start justify-between gap-2 mb-2">
        <div className="min-w-0 flex-1">
          <Type
            variant="subheading"
            as="div"
            className="truncate text-md group-hover:text-primary transition-colors"
            title={collection.name}
          >
            {collection.name}
          </Type>
        </div>
        <Badge variant="secondary">
          <Server className="w-3 h-3 mr-1" />
          {serverCount} {serverCount === 1 ? "server" : "servers"}
        </Badge>
      </div>

      {collection.description && (
        <Type small muted className="line-clamp-2 mb-3">
          {collection.description}
        </Type>
      )}

      <div className="flex items-center justify-end gap-2 mt-auto pt-2">
        {collection.visibility === "public" && (
          <Stack
            direction="horizontal"
            gap={1}
            align="center"
            className="text-muted-foreground mr-auto"
          >
            <Eye className="w-3.5 h-3.5" />
            <Type small muted>
              Public
            </Type>
          </Stack>
        )}

        <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
          <Button variant="secondary" size="sm">
            <Button.Text>View</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="w-4 h-4" />
            </Button.RightIcon>
          </Button>
        </Link>
      </div>
    </DotCard>
  );
}
