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
  const detailHref = orgRoutes.collections.detail.href(collection.slug ?? "");

  return (
    <DotCard
      className="cursor-pointer"
      onClick={() => navigate(detailHref)}
      icon={
        <LayoutGrid className="text-muted-foreground h-10 w-10 opacity-60" />
      }
      overlay={
        collection.visibility === "private" ? (
          <div className="absolute top-3.5 left-3.5 z-10">
            <Badge
              variant="outline"
              className="border-muted-foreground/30 bg-background/80 text-muted-foreground backdrop-blur-sm"
            >
              <Lock className="mr-1 h-3 w-3" />
              Private
            </Badge>
          </div>
        ) : undefined
      }
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary truncate transition-colors"
            title={collection.name}
          >
            {collection.name}
          </Type>
        </div>
        <Badge variant="secondary">
          <Server className="mr-1 h-3 w-3" />
          {serverCount} {serverCount === 1 ? "server" : "servers"}
        </Badge>
      </div>

      {collection.description && (
        <Type small muted className="mb-3 line-clamp-2">
          {collection.description}
        </Type>
      )}

      <div className="mt-auto flex items-center justify-end gap-2 pt-2">
        {collection.visibility === "public" && (
          <Stack
            direction="horizontal"
            gap={1}
            align="center"
            className="text-muted-foreground mr-auto"
          >
            <Eye className="h-3.5 w-3.5" />
            <Type small muted>
              Public
            </Type>
          </Stack>
        )}

        <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
          <Button variant="secondary" size="sm">
            <Button.Text>View</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        </Link>
      </div>
    </DotCard>
  );
}
