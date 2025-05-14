import { cn } from "@/lib/utils.ts";
import { PageHeader } from "./page-header.tsx";

function PageLayout({ children }: { children: React.ReactNode }) {
  return <div className="flex flex-col overflow-hidden">{children}</div>;
}

function PageBody({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "@container/main flex flex-col gap-4 p-8 pb-0 overflow-y-auto h-full",
        className
      )}
    >
      {children}
    </div>
  );
}

export const Page = Object.assign(PageLayout, {
  Header: PageHeader,
  Body: PageBody,
});
