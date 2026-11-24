import type { ReactNode } from "react";
import { ChevronDown } from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Text } from "@/components/ui/Text";

/**
 * Protocol detail most people never need, closed by default under one
 * "Technical details" toggle so the plain explanation above it reads first.
 */
export function TechnicalDetails({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  return (
    <Collapsible>
      <CollapsibleTrigger className="group text-muted-foreground flex items-center gap-1 text-sm underline underline-offset-4">
        <ChevronDown
          aria-hidden="true"
          className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
        />
        Technical details
      </CollapsibleTrigger>
      <CollapsibleContent className="pt-2">
        <Text muted small className="max-w-3xl">
          {children}
        </Text>
      </CollapsibleContent>
    </Collapsible>
  );
}
