import { cn } from "@/lib/utils";
import { Badge } from "./ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { Type } from "./ui/type";

export const HttpRoute = ({
  method,
  path,
}: {
  method: string;
  path: string;
}) => {
  return (
    <div className="flex gap-2 font-mono items-start">
      <HttpMethod method={method} variant="type" />
      <Type className="text-xs text-muted-foreground wrap-anywhere">
        {path}
      </Type>
    </div>
  );
};

export const HttpMethodColors: Record<
  string,
  { bg: string; text: string; border: string }
> = {
  GET: {
    bg: "bg-blue-300! dark:bg-blue-800!",
    text: "text-blue-600! dark:text-blue-400!",
    border: "border-blue-300! dark:border-blue-800!",
  },
  POST: {
    bg: "bg-emerald-300! dark:bg-emerald-800!",
    text: "text-emerald-600! dark:text-emerald-400!",
    border: "border-emerald-300! dark:border-emerald-800!",
  },
  PATCH: {
    bg: "bg-amber-300! dark:bg-amber-800!",
    text: "text-amber-600! dark:text-amber-300!",
    border: "border-amber-300! dark:border-amber-800!",
  },
  PUT: {
    bg: "bg-amber-300! dark:bg-amber-800!",
    text: "text-amber-600! dark:text-amber-300!",
    border: "border-amber-300! dark:border-amber-800!",
  },
  DELETE: {
    bg: "bg-rose-300! dark:bg-rose-800!",
    text: "text-rose-600! dark:text-rose-400!",
    border: "border-rose-300! dark:border-rose-800!",
  },
};

const HttpMethod = ({ method }: { method: string }) => {
  const typeStyle = HttpMethodColors[method]?.text;

  return (
    <Type className={cn("text-xs font-semibold text-nowrap", typeStyle)}>
      {method}
    </Type>
  );
};
