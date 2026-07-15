import { cn } from "@/lib/utils";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "./ui/accordion";

function Expandable({
  children,
  className,
  defaultExpanded,
}: {
  children: React.ReactNode;
  className?: string;
  defaultExpanded?: boolean;
}): React.JSX.Element {
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
        "w-full items-center border-1 px-4 py-2 text-base [&[data-state=open]]:rounded-b-none",
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
        "bg-background h-48 overflow-y-auto rounded-b-md border-1 border-t-0 p-4",
        className,
      )}
    >
      {children}
    </AccordionContent>
  );
}

// Compound members are attached by mutation rather than Object.assign: the
// react/only-export-components rule recognizes the former as a component
// export and flags the latter.
Expandable.Trigger = ExpandableTrigger;
Expandable.Content = ExpandableContent;

export { Expandable };
