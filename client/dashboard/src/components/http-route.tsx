import { cn } from "@/lib/utils";
import { HttpMethodColors } from "@/components/http-method-colors";
import { Type } from "./ui/type";

export const HttpRoute = ({
  method,
  path,
  className,
}: {
  method: string;
  path: string;
  className?: string;
}) => {
  return (
    <div className={cn("flex items-start gap-2 font-mono", className)}>
      <HttpMethod method={method} />
      <Type className="text-muted-foreground text-xs wrap-anywhere">
        {path}
      </Type>
    </div>
  );
};

const HttpMethod = ({ method }: { method: string }) => {
  const typeStyle = HttpMethodColors[method]?.text;

  return (
    <Type className={cn("text-xs font-semibold text-nowrap", typeStyle)}>
      {method}
    </Type>
  );
};
