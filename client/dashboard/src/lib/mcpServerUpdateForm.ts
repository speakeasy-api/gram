import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import {
  type UpdateMcpServerForm,
  UpdateMcpServerFormVisibility,
} from "@gram/client/models/components/updatemcpserverform.js";
import { isKnownEnumValue } from "@/lib/enum";

// Network access is omitted so the stored mode is kept without re-running private admission.
export function mcpServerUpdateForm(latest: McpServer): Omit<
  UpdateMcpServerForm,
  "visibility"
> & {
  visibility: UpdateMcpServerFormVisibility | undefined;
} {
  return {
    id: latest.id,
    environmentId: latest.environmentId,
    remoteMcpServerId: latest.remoteMcpServerId,
    tunneledMcpServerId: latest.tunneledMcpServerId,
    toolsetId: latest.toolsetId,
    unproxiedMcpServerId: latest.unproxiedMcpServerId,
    toolVariationsGroupId: latest.toolVariationsGroupId,
    visibility: isKnownEnumValue(
      UpdateMcpServerFormVisibility,
      latest.visibility,
    )
      ? latest.visibility
      : undefined,
  };
}
