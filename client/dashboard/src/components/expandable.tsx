import { cn } from "@/lib/utils";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "./ui/accordion";

function ExpandableComponent({
  children,
  className,
  defaultExpanded,
}: {
  children: React.ReactNode;
  className?: string;
  defaultExpanded?: boolean;
}) {
  return (
    <Accordion
      type="single"
      collapsible
      className={className}
      defaultValue={defaultExpanded ? "logs" : undefined}
    >
      <AccordionItem value="logs">{children}</AccordionItem>
    </Accordion>
  );
}

function ExpandableTrigger({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <AccordionTrigger
      className={cn(
        "text-base border-1 px-4 py-2 w-full [&[data-state=open]]:rounded-b-none items-center",
        className,
      )}
    >
      {children}
    </AccordionTrigger>
  );
}

function ExpandableContent({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <AccordionContent
      className={cn(
        "p-4 bg-background h-48 overflow-y-auto border-1 border-t-0 rounded-b-md",
        className,
      )}
    >
      {children}
    </AccordionContent>
  );
}

export const Expandable = Object.assign(ExpandableComponent, {
  Trigger: ExpandableTrigger,
  Content: ExpandableContent,
});
