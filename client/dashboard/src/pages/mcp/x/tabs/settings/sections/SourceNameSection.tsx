import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useUpdateRemoteMcpServerMutation } from "@gram/client/react-query/updateRemoteMcpServer.js";
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { EditableSourceFieldSection } from "./EditableSourceFieldSection";
import {
  invalidateRemoteMcpSourceViews,
  invalidateTunneledMcpSourceViews,
} from "./sourceInvalidation";

export const MCP_SOURCE_NAME_SECTION_ID = "source-name";

// The MCP server's display name (Branding, above) is what people installing
// the server see. The source has a name of its own, shown on the sources shelf
// and used to derive tunnel agent snippets, and it is edited here so the two
// never get confused for one another.
const SOURCE_NAME_DESCRIPTION =
  "The name of the source behind this MCP server, as listed on the sources " +
  "shelf. This is separate from the display name under Branding, which is " +
  "what people installing the server see.";

export function RemoteSourceNameSection({
  remoteMcpServer,
}: {
  remoteMcpServer: RemoteMcpServer;
}): JSX.Element {
  const update = useUpdateRemoteMcpServerMutation();
  const queryClient = useQueryClient();

  return (
    <EditableSourceFieldSection
      id={MCP_SOURCE_NAME_SECTION_ID}
      title="Source Name"
      description={`${SOURCE_NAME_DESCRIPTION} Leave blank to fall back to the upstream URL.`}
      label="Remote source name"
      placeholder="My MCP server"
      stored={remoteMcpServer.name ?? ""}
      projectId={remoteMcpServer.projectId}
      footerHint="Optional. Defaults to the upstream URL when empty."
      save={async (value) => {
        // Empty string explicitly clears the name on the server side; nil
        // would leave it unchanged.
        const saved = await update.mutateAsync({
          request: {
            updateServerForm: { id: remoteMcpServer.id, name: value },
          },
        });
        await invalidateRemoteMcpSourceViews(queryClient);
        return saved.name ?? "";
      }}
      toastMessage={(cleared) =>
        cleared ? "Source name cleared" : "Source name updated"
      }
      fallbackError="Failed to update source name"
    />
  );
}

export function TunneledSourceNameSection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}): JSX.Element {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();

  return (
    <EditableSourceFieldSection
      id={MCP_SOURCE_NAME_SECTION_ID}
      title="Source Name"
      description={`${SOURCE_NAME_DESCRIPTION} Tunnel agent setup snippets derive their slug from it.`}
      label="Tunneled source name"
      placeholder="Internal MCP server"
      stored={tunneledMcpServer.name}
      projectId={tunneledMcpServer.projectId}
      requireValue
      footerHint="Required."
      save={async (value) => {
        const saved = await update.mutateAsync({
          request: {
            updateTunneledMcpServerForm: {
              id: tunneledMcpServer.id,
              name: value,
            },
          },
        });
        await invalidateTunneledMcpSourceViews(queryClient);
        return saved.name;
      }}
      toastMessage={() => "Source name updated"}
      fallbackError="Failed to update source name"
    />
  );
}
