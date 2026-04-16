import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Badge } from "@speakeasy-api/moonshine";
import { Network, ScrollText } from "lucide-react";

const BUILT_IN_ICONS: Record<string, React.ReactNode> = {
  logs: <ScrollText className="text-muted-foreground h-8 w-8" />,
};

interface BuiltInMCPCardProps {
  name: string;
  description: string;
  slug: string;
}

export function BuiltInMCPCard({
  name,
  description,
  slug,
}: BuiltInMCPCardProps) {
  const routes = useRoutes();

  return (
    <DotCard
      className="cursor-pointer"
      onClick={() => routes.mcp.builtIn.goTo(slug)}
      icon={
        BUILT_IN_ICONS[slug] ?? (
          <Network className="text-muted-foreground h-8 w-8" />
        )
      }
    >
      {/* Header row with name and badge */}
      <div className="mb-2 flex items-start justify-between gap-2">
        <Type
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary flex-1 truncate transition-colors"
          title={name}
        >
          {name}
        </Type>
        <Badge variant="information">
          <Badge.Text>Built-in</Badge.Text>
        </Badge>
      </div>

      {/* Description */}
      <Type variant="small" muted className="line-clamp-2">
        {description}
      </Type>
    </DotCard>
  );
}
