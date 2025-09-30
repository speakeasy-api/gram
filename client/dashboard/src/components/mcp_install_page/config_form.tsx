import type { Toolset } from "@gram/client/models/components";
import {
  useGetInstallPageMetadata,
  useMcpInstallPageSetMutation,
  invalidateGetInstallPageMetadata,
} from "@gram/client/react-query";
import { Stack, Input, cn } from "@speakeasy-api/moonshine";
import { CompactUpload, useAssetImageUploadHandler } from "../upload";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import {
  ChangeEventHandler,
  FocusEventHandler,
  useEffect,
  useState,
} from "react";
import { AssetImage } from "../asset-image";
import { GramError } from "@gram/client/models/errors";
import { useQueryClient } from "@tanstack/react-query";

interface ConfigFormProps {
  toolset: Toolset;
}

interface ExternalDocumentationUrlInputHandlers {
  value: string;
  error?: boolean;
  onChange: ChangeEventHandler<HTMLInputElement>;
  onBlur: FocusEventHandler<HTMLInputElement>;
}

function useExternalDocumentationUrlHandlers(
  value: string | undefined,
  setValue: (nextDocumentationUrl: string) => void,
): ExternalDocumentationUrlInputHandlers {
  const [urlValue, setUrlValue] = useState(value ?? "");
  const [valid, setValid] = useState(true);

  useEffect(() => setUrlValue(value ?? ''), [value])

  useEffect(() => {
    try {
      new URL(urlValue);
      setValid(true);
    } catch (err) {
      setValid(false);
    }
  }, [urlValue]);

  return {
    value: urlValue,
    error: value && value.length > 0 ? !valid : undefined,
    onChange: (e) => setUrlValue(e.target.value),
    onBlur: () => {
      if (valid) {
        setValue(urlValue);
      }
    },
  };
}

export function ConfigForm({ toolset }: ConfigFormProps) {
  const queryClient = useQueryClient();

  const result = useGetInstallPageMetadata(
    { toolsetId: toolset.id },
    undefined,
    {
      retry: (_failCount, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return true;
      },
      throwOnError: false,
    },
  );

  const [metadataParams, setMetadataParams] = useState<{
    logoAssetId: string | undefined;
    externalDocumentationUrl: string | undefined;
  }>({
    externalDocumentationUrl:
      result.data?.metadata?.externalDocumentationUrl ?? undefined,
    logoAssetId: result.data?.metadata?.logoAssetId ?? undefined,
  });

  const mutation = useMcpInstallPageSetMutation({
    onSettled: () => {
      invalidateGetInstallPageMetadata(queryClient, [
        { toolsetId: toolset.id },
      ]);
    },
  });

  useEffect(() => {
    if (
      metadataParams.externalDocumentationUrl !==
        result.data?.metadata?.externalDocumentationUrl ||
      metadataParams.logoAssetId !== result.data?.metadata?.logoAssetId
    ) {
      setMetadataParams({
        externalDocumentationUrl:
          result.data?.metadata?.externalDocumentationUrl ?? undefined,
        logoAssetId: result.data?.metadata?.logoAssetId ?? undefined,
      });
    }
  }, [result.data?.metadata]);

  useEffect(() => {
    mutation.mutate({
      request: {
        setInstallPageMetadataRequestBody: {
          toolsetId: toolset.id,
          ...metadataParams,
        },
      },
    });
  }, [metadataParams]);

  const uploadHandler = useAssetImageUploadHandler((assetResult) => {
    setMetadataParams({ ...metadataParams, logoAssetId: assetResult.asset.id });
  });

  const urlInputHandlers = useExternalDocumentationUrlHandlers(
    metadataParams.externalDocumentationUrl,
    (value) => {
      console.log('setting')
      setMetadataParams({
        ...metadataParams,
        externalDocumentationUrl: value,
      });
    },
  );

  return (
    <Stack
      className={cn(
        "my-2 gap-2",
        mutation.status === "pending" && "animate-pulse",
      )}
    >
      <Label> MCP Logo </Label>
      <Type muted small className="max-w-2xl">
        The logo associated with this install page
      </Type>
      <div className="inline-block">
        <CompactUpload
          onUpload={uploadHandler}
          renderFilePreview={() =>
            result.data?.metadata?.logoAssetId && (
              <AssetImage assetId={result.data?.metadata?.logoAssetId!} className="w-16 h-16" />
            )
          }
        />
      </div>
      <Label> Documentation Link </Label>
      <Type muted small className="max-w-2xl">
        A link to your own MCP documentation that will be featured at the top of
        your install page
      </Type>
      <Input
        type="text"
        placeholder="https://my-documentation.link"
        {...urlInputHandlers}
      />
    </Stack>
  );
}
