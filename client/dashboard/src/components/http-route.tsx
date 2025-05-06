import { cn } from "@/lib/utils";
import { Type } from "./ui/type";
import { Badge } from "./ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./ui/tooltip";

export const HttpRoute = ({
  method,
  path,
}: {
  method: string;
  path: string;
}) => {
  return (
    <div className="flex items-center gap-2 overflow-hidden font-mono">
      <HttpMethod method={method} variant="type" />
      <Type className="overflow-hidden text-ellipsis text-xs text-muted-foreground">
        {path}
      </Type>
    </div>
  );
};

export const HttpMethod = ({
  method,
  path,
  variant = "badge",
}: {
  method: string;
  path?: string;
  variant?: "badge" | "type";
}) => {
  if (variant === "type") {
    const typeStyle = {
      GET: "text-blue-600! dark:text-blue-400!",
      POST: "text-emerald-600! dark:text-emerald-400!",
      PATCH: "text-amber-600! dark:text-amber-300!",
      PUT: "text-amber-600! dark:text-amber-300!",
      DELETE: "text-rose-600! dark:text-rose-400!",
    }[method];

    return (
      <Type className={cn("text-xs font-semibold", typeStyle)}>{method}</Type>
    );
  }

  if (variant === "badge") {
    const badgeStyle = {
      GET: "bg-blue-300! dark:bg-blue-800!",
      POST: "bg-emerald-300! dark:bg-emerald-800!",
      PATCH: "bg-amber-300! dark:bg-amber-800!",
      PUT: "bg-amber-300! dark:bg-amber-800!",
      DELETE: "bg-rose-300! dark:bg-rose-800!",
    }[method];

    const badge = (
      <Badge
        variant="secondary"
        className={cn("text-sm capitalize", badgeStyle)}
        size="sm"
      >
        {method}
      </Badge>
    );

    if (path) {
      return (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>{badge}</TooltipTrigger>
            <TooltipContent>
              <HttpRoute method={method} path={path} />
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      );
    }

    return badge;
  }

  return null;
};
