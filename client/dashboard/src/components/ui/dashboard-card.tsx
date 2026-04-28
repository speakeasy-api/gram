import { type ReactNode } from "react";
import { Icon } from "@speakeasy-api/moonshine";
import { SimpleTooltip } from "@/components/ui/tooltip";

type DashboardCardProps = {
  title: string;
  action?: ReactNode;
  children: ReactNode;
  tooltip?: string;
};

export function DashboardCard({
  title,
  action,
  children,
  tooltip,
}: DashboardCardProps) {
  return (
    <div className="bg-card text-card-foreground relative flex h-full w-full flex-col rounded-lg border">
      <div className="flex w-full flex-row items-center justify-between gap-4 border-b px-6 py-4">
        <div className="flex items-center gap-1.5">
          <h3 className="text-sm font-semibold">{title}</h3>
          {tooltip && (
            <SimpleTooltip tooltip={tooltip}>
              <button
                type="button"
                aria-label={`About ${title}`}
                className="text-muted-foreground hover:text-foreground inline-flex cursor-help items-center"
              >
                <Icon name="info" className="size-3.5" />
              </button>
            </SimpleTooltip>
          )}
        </div>
        {action}
      </div>
      <div className="px-6 py-5">{children}</div>
    </div>
  );
}
