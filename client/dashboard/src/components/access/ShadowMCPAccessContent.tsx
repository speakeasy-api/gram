import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";

export function ShadowMCPAccessContent() {
  return (
    <div>
      <Heading variant="h4">Shadow MCP</Heading>
      <Type muted small className="mt-1">
        Review blocked Shadow MCP requests and manage Access Rules.
      </Type>
    </div>
  );
}
