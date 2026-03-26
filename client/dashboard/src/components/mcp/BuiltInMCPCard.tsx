import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Badge } from "@speakeasy-api/moonshine";
import { Network, ScrollText } from "lucide-react";

const BUILT_IN_ICONS: Record<string, React.ReactNode> = {
  logs: <ScrollText className="w-8 h-8 text-muted-foreground" />,
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
          <Network className="w-8 h-8 text-muted-foreground" />
        )
      }
    >
      {/* Header row with name and badge */}
      <div className="flex items-start justify-between gap-2 mb-2">
        <Type
          variant="subheading"
          as="div"
          className="truncate flex-1 text-md group-hover:text-primary transition-colors"
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
