import type { Toolset } from "@gram/client/models/components";
import { Stack, Input } from "@speakeasy-api/moonshine";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { CompactUpload, useAssetImageUploadHandler } from "../upload";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { useState } from "react";
import { AssetImage } from "../asset-image";

interface ConfigFormProps {
  toolset: Toolset;
}

function delay(ms: number): Promise<void> {
  return new Promise<void>((resolve) => {
    setTimeout(() => resolve(), ms);
  });
}

let assetId: string | null = null;

interface InstallPageConfig {
  assetId: string;
  docsUrl: string;
}

function useInstallPageConfigSuspense({
  toolsetSlug,
}: {
  toolsetSlug: string;
}) {
  return useSuspenseQuery<InstallPageConfig | null>({
    queryKey: ["toolset", toolsetSlug, "install-page-config"],
    queryFn: async () => {
      await delay(300);
      if (assetId === null) {
        return null;
      }
      return {
        assetId,
        docsUrl: "https://example.com",
      };
    },
  });
}

function useCreateOrUpdateInstallPageMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({
      toolsetSlug,
      assetId: mutAssetId,
    }: {
      toolsetSlug: string;
      assetId: string;
    }) => {
      assetId = mutAssetId;
      await delay(500);
      return { assetId: assetId, toolsetSlug };
    },
    onSuccess: (res) => {
      console.log("calling config success handler");
      queryClient.invalidateQueries({
        queryKey: ["toolset", res.toolsetSlug, "install-page-config"],
      });
    },
  });
}

export function ConfigForm({ toolset }: ConfigFormProps) {
  const result = useInstallPageConfigSuspense({ toolsetSlug: toolset.slug });
  const mutation = useCreateOrUpdateInstallPageMutation();
  const [installLink, setInstallLink] = useState(result.data?.docsUrl ?? null);
  const handler = useAssetImageUploadHandler((asset) => {
    mutation.mutate({ toolsetSlug: toolset.slug, assetId: asset.asset.id });
  });

  const renderAssetPreviewOrUndefined = () => {
    if (!result.data?.assetId) return undefined;
    return () => <AssetImage assetId={result.data?.assetId!} />;
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
