import {
  HeadersEditor,
  type EditableHeader,
  type HeaderWriteFields,
  type HeadersEditorAdapter,
} from "@/components/headers-editor";
import { useCreateTunneledMcpServerHeaderMutation } from "@gram/client/react-query/createTunneledMcpServerHeader.js";
import { useDeleteTunneledMcpServerHeaderMutation } from "@gram/client/react-query/deleteTunneledMcpServerHeader.js";
import {
  invalidateAllTunneledMcpServerHeaders,
  useTunneledMcpServerHeaders,
} from "@gram/client/react-query/tunneledMcpServerHeaders.js";
import { useUpdateTunneledMcpServerHeaderMutation } from "@gram/client/react-query/updateTunneledMcpServerHeader.js";
import { useQueryClient } from "@tanstack/react-query";

export function HeadersSection({
  tunneledMcpServerId,
}: {
  tunneledMcpServerId: string;
}): JSX.Element {
  const headersQuery = useTunneledMcpServerHeaders(
    { tunneledMcpServerId },
    undefined,
    { enabled: tunneledMcpServerId !== "" },
  );

  const queryClient = useQueryClient();
  const createHeader = useCreateTunneledMcpServerHeaderMutation();
  const updateHeader = useUpdateTunneledMcpServerHeaderMutation();
  const deleteHeader = useDeleteTunneledMcpServerHeaderMutation();

  const adapter: HeadersEditorAdapter = {
    headers: headersQuery.data?.headers,
    isLoading: headersQuery.isLoading,
    isSaving:
      createHeader.isPending ||
      updateHeader.isPending ||
      deleteHeader.isPending,
    mutationError:
      createHeader.error ?? updateHeader.error ?? deleteHeader.error,
    createHeader: async (fields: HeaderWriteFields) => {
      await createHeader.mutateAsync({
        request: {
          createTunneledMcpServerHeaderForm: { tunneledMcpServerId, ...fields },
        },
      });
    },
    updateHeader: async (id: string, fields: HeaderWriteFields) => {
      await updateHeader.mutateAsync({
        request: { updateTunneledMcpServerHeaderForm: { id, ...fields } },
      });
    },
    deleteHeader: async (id: string) => {
      await deleteHeader.mutateAsync({ request: { id } });
    },
    refetch: async (): Promise<EditableHeader[] | null> => {
      const refreshed = await headersQuery.refetch();
      if (refreshed.isError || !refreshed.data) return null;
      return refreshed.data.headers ?? [];
    },
    invalidate: async () => {
      await invalidateAllTunneledMcpServerHeaders(queryClient, {
        refetchType: "all",
      });
    },
  };

  return (
    <HeadersEditor
      adapter={adapter}
      title="Upstream Headers"
      description="Headers sent through the tunnel to your MCP server."
    />
  );
}
