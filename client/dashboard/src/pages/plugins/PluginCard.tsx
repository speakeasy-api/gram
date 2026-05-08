import { Badge } from "@/components/ui/badge";
import { DotCard } from "@/components/ui/dot-card";
import { MoreActions } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import { Plugin } from "@gram/client/models/components";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { ArrowRight, Puzzle, Server } from "lucide-react";
import { Link, useNavigate } from "react-router";

export function PluginCard({
  plugin,
  onDelete,
}: {
  plugin: Plugin;
  onDelete: (plugin: Plugin) => void;
}) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const detailHref = routes.plugins.detail.href(plugin.id);
  const serverCount = plugin.serverCount ?? 0;

  return (
    <DotCard
      className="cursor-pointer"
      onClick={() => navigate(detailHref)}
      icon={<Puzzle className="text-muted-foreground h-10 w-10 opacity-60" />}
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary truncate transition-colors"
            title={plugin.name}
          >
            {plugin.name}
          </Type>
          <Type small muted className="truncate font-mono" title={plugin.slug}>
            {plugin.slug}
          </Type>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Badge variant="secondary">
            <Server className="mr-1 h-3 w-3" />
            {serverCount} {serverCount === 1 ? "server" : "servers"}
          </Badge>
          <div onClick={(e) => e.stopPropagation()}>
            <MoreActions
              actions={[
                {
                  label: "Delete",
                  icon: "trash-2",
                  destructive: true,
                  onClick: () => onDelete(plugin),
                },
              ]}
            />
          </div>
        </div>
      </div>

      {plugin.description && (
        <Type small muted className="mb-3 line-clamp-2">
          {plugin.description}
        </Type>
      )}

      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <Stack
          direction="horizontal"
          gap={1}
          align="center"
          className="text-muted-foreground"
        >
          <Type small muted>
            Updated <HumanizeDateTime date={plugin.updatedAt} />
          </Type>
        </Stack>

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
