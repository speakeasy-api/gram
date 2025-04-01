import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Heading } from "./ui/heading.tsx";

export function PageHeader({
  title,
  actions,
}: {
  title: string;
  actions?: React.ReactNode;
}) {
  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)">
      <div className="flex w-full items-center px-3 gap-3">
        <SidebarTrigger className="-ml-1 mx-0 px-0" />
        <Separator
          orientation="vertical"
          className="data-[orientation=vertical]:h-4"
        />
        <Heading variant="h4" className="ml-1">{title}</Heading>
        <div className="ml-auto flex items-center gap-2">{actions}</div>
      </div>
    </header>
  );
}
