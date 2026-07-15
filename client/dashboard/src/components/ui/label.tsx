"use client";

import * as React from "react";
import * as LabelPrimitive from "@radix-ui/react-label";

import { cn } from "@/lib/utils";

// Claude Design brandbook gives *field* labels a mono/uppercase/tracked
// treatment (see FieldLabel in field.tsx), but this base Label is also used
// for sentence-length copy beside checkboxes/radios/switches (e.g. "Accept
// terms and conditions", "Type the project name to confirm:") where an
// all-caps tracked treatment would read as shouting. So the mono treatment
// is intentionally NOT the default here — only FieldLabel opts into it.
function Label({
  className,
  ...props
}: React.ComponentProps<typeof LabelPrimitive.Root>): React.JSX.Element {
  return (
    <LabelPrimitive.Root
      data-slot="label"
      className={cn(
        "flex items-center gap-2 text-sm leading-none font-medium select-none group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50 peer-disabled:cursor-not-allowed peer-disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export { Label };
