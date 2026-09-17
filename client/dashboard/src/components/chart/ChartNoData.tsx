import { Badge } from "@/components/ui/Badge";
import { Icon } from "@/components/ui/Icon";

export function ChartNoData({
  message = "No data in this period",
}: {
  message?: string;
}): JSX.Element {
  return (
    <div className="flex h-24 items-center justify-center">
      <Badge variant="neutral">
        <Badge.LeftIcon>
          <Icon name="chart-no-axes-column" size="small" />
        </Badge.LeftIcon>
        <Badge.Text>{message}</Badge.Text>
      </Badge>
    </div>
  );
}
