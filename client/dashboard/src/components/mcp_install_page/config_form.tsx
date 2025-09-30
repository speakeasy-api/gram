import type { Toolset } from "@gram/client/models/components";
import {
  useGetInstallPageMetadataSuspense,
  useMcpInstallPageSetMutation
} from '@gram/client/react-query'
import { Stack, Input } from "@speakeasy-api/moonshine";
import { useQueryClient, } from "@tanstack/react-query";
import { CompactUpload, useAssetImageUploadHandler } from "../upload";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useState } from "react";
import { AssetImage } from "../asset-image";

interface ConfigFormProps {
  toolset: Toolset;
}

/*
function useCreateOrUpdateInstallPageMutation(toolsetId: string) {
  const queryClient = useQueryClient();
  return useMcpInstallPageSetMutation({
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: ["toolset", toolsetId, "install-page-config"],
      });
    }
  })
}
*/

export function ConfigForm({ toolset }: ConfigFormProps) {
  const result = useGetInstallPageMetadataSuspense({ toolsetId: toolset.id });
  const mutation = useMcpInstallPageSetMutation();
  const [installLink, setInstallLink] = useState(result.data?.metadata?.externalDocumentationUrl ?? null);
  const handler = useAssetImageUploadHandler((assetResult) => {
    mutation.mutate({
      request: {
        setInstallPageMetadataRequestBody: {
          toolsetId: toolset.id,
          externalDocumentationUrl: installLink ?? undefined,
          logoAssetId: assetResult.asset.id
        }
      }
    });
  });

  const renderAssetPreviewOrUndefined = () => {
    if (!result.data?.metadata?.logoAssetId) return undefined;
    return () => <AssetImage assetId={result.data?.metadata?.logoAssetId!} />;
  };

  return (
    <Stack className="my-2 gap-2">
      <Label> MCP Logo </Label>
      <Type muted small className="max-w-2xl">
        The logo associated with this install page
      </Type>
      <CompactUpload
        onUpload={handler}
        renderFilePreview={renderAssetPreviewOrUndefined()}
      />
      <Label> Documentation Link </Label>
      <Type muted small className="max-w-2xl">
        A link to your own MCP documentation that will be featured at the top of
        your install page
      </Type>
      <Input
        type="text"
        value={installLink ?? ""}
        onChange={(e) => setInstallLink(e.target.value)}
      />
    </Stack>
  );
}
