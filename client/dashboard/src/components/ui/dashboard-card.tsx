import { type ReactNode } from "react";

type DashboardCardProps = {
  title: string;
  action?: ReactNode;
  children: ReactNode;
};

export function DashboardCard({ title, action, children }: DashboardCardProps) {
  return (
    <div className="bg-card text-card-foreground relative flex h-full w-full flex-col rounded-lg border">
      <div className="flex w-full flex-row items-center justify-between gap-4 border-b px-6 py-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        {action}
      </div>
      <div className="px-6 py-5">{children}</div>
    </div>
  );
}
