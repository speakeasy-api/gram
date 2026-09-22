import { useState, type JSX } from "react";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { agentMatrixPrompt } from "./agentExport";
import type { Catalog, Draft } from "./model";

export function CopyForAgents({
  catalog,
  draft,
}: {
  catalog: Catalog;
  draft: Draft;
}): JSX.Element {
  const [copying, setCopying] = useState(false);
  async function copy() {
    setCopying(true);
    try {
      await navigator.clipboard.writeText(agentMatrixPrompt(catalog, draft));
      toast.success("Complete matrix copied for agents");
    } catch {
      toast.error(
        "Could not copy the matrix. Check clipboard permissions and try again.",
      );
    } finally {
      setCopying(false);
    }
  }
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={copying}
      onClick={() => void copy()}
      title="Copy the complete matrix as agent-ready data, regardless of filters"
    >
      <Copy className="size-4" />
      {copying ? "Copying…" : "Copy for agents"}
    </Button>
  );
}
