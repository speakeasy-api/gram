import { cn } from "@/lib/utils";
import { HttpMethodColors } from "@/components/http-method-colors";
import { Text } from "@/components/ui/Text";

export const HttpRoute = ({
  method,
  path,
  className,
}: {
  method: string;
  path: string;
  className?: string;
}): JSX.Element => {
  return (
    <div className={cn("flex items-start gap-2 font-mono", className)}>
      <HttpMethod method={method} />
      <Text className="text-muted-foreground text-xs wrap-anywhere">
        {path}
      </Text>
    </div>
  );
};

const HttpMethod = ({ method }: { method: string }) => {
  const typeStyle = HttpMethodColors[method]?.text;

  return (
    <Text className={cn("text-xs font-semibold text-nowrap", typeStyle)}>
      {method}
    </Text>
  );
};
