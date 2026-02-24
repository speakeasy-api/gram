import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Badge } from "@speakeasy-api/moonshine";
import { MCPPatternIllustration } from "../sources/SourceCardIllustrations";

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
    <div
      className="group bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden hover:border-foreground/20 hover:shadow-md transition-all cursor-pointer"
      onClick={() => routes.mcp.builtIn.goTo(slug)}
    >
      {/* Illustration header */}
      <div className="h-36 w-full overflow-hidden border-b">
        <MCPPatternIllustration
          toolsetSlug={`built-in-${slug}`}
          className="saturate-[.3] group-hover:saturate-100 transition-all duration-300"
        />
      </div>

      {/* Content area */}
      <div className="p-4 flex flex-col flex-1">
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
      </div>
    </div>
  );
}
