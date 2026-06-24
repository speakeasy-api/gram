// Policy-kind chooser shown in create mode when prompt policies are enabled
// (AGE-2704). Moved verbatim from PolicyCenter.tsx.

import { Type } from "@/components/ui/type";
import { Badge } from "@speakeasy-api/moonshine";
import { ChevronRight, Shield, Sparkles } from "lucide-react";
import type { PolicyKind } from "./payload";

export function PolicyKindChoice({
  onSelect,
}: {
  onSelect: (kind: PolicyKind) => void;
}): JSX.Element {
  return (
    <div className="space-y-3">
      <button
        type="button"
        onClick={() => onSelect("risk")}
        className="border-border hover:bg-muted/50 focus-visible:ring-ring flex w-full items-start gap-3 rounded-lg border p-4 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none"
      >
        <div className="bg-muted mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md">
          <Shield className="text-muted-foreground h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <Type className="font-medium">Standard</Type>
          <Type small muted className="mt-0.5">
            Catch secrets, PII, and destructive commands using built-in scanners
            and custom regex rules.
          </Type>
        </div>
        <ChevronRight className="text-muted-foreground mt-2.5 h-4 w-4 shrink-0" />
      </button>
      <button
        type="button"
        onClick={() => onSelect("prompt")}
        className="border-border hover:bg-muted/50 focus-visible:ring-ring flex w-full items-start gap-3 rounded-lg border p-4 text-left transition-colors focus-visible:ring-2 focus-visible:outline-none"
      >
        <div className="bg-muted mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-md">
          <Sparkles className="text-muted-foreground h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Type className="font-medium">Prompt-based</Type>
            <Badge variant="neutral" className="text-[10px]">
              <Badge.Text>New</Badge.Text>
            </Badge>
          </div>
          <Type small muted className="mt-0.5">
            Describe any behavior you want to detect in plain language. No
            scanner configuration needed.
          </Type>
        </div>
        <ChevronRight className="text-muted-foreground mt-2.5 h-4 w-4 shrink-0" />
      </button>
    </div>
  );
}
