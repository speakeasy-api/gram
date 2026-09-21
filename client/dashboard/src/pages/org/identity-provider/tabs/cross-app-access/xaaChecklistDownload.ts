import {
  downloadBlob,
  filenameFromContentDisposition,
  headerValue,
} from "@/lib/download";
import type { GramCore } from "@gram/client/core.js";
import { oktaResourceConnectionsExportChecklist } from "@gram/client/funcs/oktaResourceConnectionsExportChecklist.js";

import { SESSION_SECURITY } from "../../identityProviderQueries";

export type ChecklistFormat = "csv" | "markdown";

export async function downloadXaaChecklist(
  client: GramCore,
  format: ChecklistFormat,
  includeAll: boolean,
): Promise<void> {
  const response = await oktaResourceConnectionsExportChecklist(
    client,
    { format, includeAll },
    SESSION_SECURITY,
  );
  if (!response.ok) throw response.error;

  const { headers, result } = response.value;
  const blob = await new Response(result).blob();
  const fallback = format === "csv" ? "xaa-checklist.csv" : "xaa-checklist.md";
  downloadBlob(
    blob,
    filenameFromContentDisposition(
      headerValue(headers, "content-disposition"),
      fallback,
    ),
  );
}
