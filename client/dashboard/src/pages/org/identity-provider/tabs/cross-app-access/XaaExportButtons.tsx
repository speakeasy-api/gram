import { useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/Button";
import { describeApiError } from "@/lib/api-error";
import { useGramContext } from "@gram/client/react-query/_context.js";

import {
  type ChecklistFormat,
  downloadXaaChecklist,
} from "./xaaChecklistDownload";

export function XaaExportButtons({
  includeAll,
}: {
  includeAll: boolean;
}): JSX.Element {
  const client = useGramContext();
  const [exporting, setExporting] = useState<ChecklistFormat | null>(null);

  const run = async (format: ChecklistFormat) => {
    setExporting(format);
    try {
      await downloadXaaChecklist(client, format, includeAll);
    } catch (error) {
      toast.error(describeApiError(error).message);
    } finally {
      setExporting(null);
    }
  };

  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        disabled={exporting != null}
        onClick={() => void run("csv")}
      >
        {exporting === "csv" ? "Exporting..." : "Export CSV"}
      </Button>
      <Button
        variant="secondary"
        size="sm"
        disabled={exporting != null}
        onClick={() => void run("markdown")}
      >
        {exporting === "markdown" ? "Exporting..." : "Export Markdown"}
      </Button>
    </>
  );
}
