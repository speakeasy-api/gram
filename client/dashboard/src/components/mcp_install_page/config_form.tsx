import type {
  MCPInstallPageMetadata,
  Toolset,
} from "@gram/client/models/components";
import {
  useGetInstallPageMetadata,
  useMcpInstallPageSetMutation,
  invalidateGetInstallPageMetadata,
} from "@gram/client/react-query";
import { Button, Stack, Input, cn, Icon } from "@speakeasy-api/moonshine";
import { Link } from "@/components/ui/link";
import { CompactUpload, useAssetImageUploadHandler } from "../upload";
import { Label as Heading } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import {
  ChangeEventHandler,
  useCallback,
  useEffect,
  useState,
} from "react";
import { AssetImage } from "../asset-image";
import { GramError } from "@gram/client/models/errors";
import { useQueryClient } from "@tanstack/react-query";
import { CodeBlock } from "@/components/code";
import { useMcpUrl } from "@/pages/mcp/MCPDetails";

interface ConfigFormProps {
  toolset: Toolset;
}

interface ExternalDocumentationUrlInputHandlers {
  value: string | undefined;
  error?: boolean;
  onChange: ChangeEventHandler<HTMLInputElement>;
}

interface MetadataParams {
  logoAssetId: string | undefined;
  externalDocumentationUrl: string | undefined;
}

function useExternalDocumentationUrlHandlers(
  value: string | undefined,
  setValue: (nextDocumentationUrl: string | undefined) => void,
): ExternalDocumentationUrlInputHandlers {
  const [valid, setValid] = useState(true);

  useEffect(() => {
    if (!value) {
      setValid(true)
      return
    }
    try {
      new URL(value);
      setValid(true);
    } catch (err) {
      setValid(false);
    }
  }, [value]);

  return {
    value,
    error: value && value.length > 0 ? !valid : undefined,
    onChange: (e) => setValue(e.target.value === '' ? undefined : e.target.value),
  };
}

function shouldUpdate(
  requestData: MetadataParams,
  metadata?: MCPInstallPageMetadata,
) {
  if (
    (!metadata && (requestData.logoAssetId || requestData.externalDocumentationUrl))) {
    return true
  }

  if (metadata) {
    if (
      metadata.logoAssetId !== requestData.logoAssetId ||
      metadata.externalDocumentationUrl !==
        requestData.externalDocumentationUrl
    ) {
      console.log('cats')
      return true;
    }
  }
  return false;
}

export function ConfigForm({ toolset }: ConfigFormProps) {
  const queryClient = useQueryClient();
  const { url: mcpUrl } = useMcpUrl(toolset);

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

  const [metadataParams, setMetadataParams] = useState<MetadataParams>({
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

  const handleUpload = useAssetImageUploadHandler((assetResult) => {
    setMetadataParams({ ...metadataParams, logoAssetId: assetResult.asset.id });
  });

  const urlInputHandlers = useExternalDocumentationUrlHandlers(
    metadataParams.externalDocumentationUrl,
    (value) => {
      setMetadataParams({
        ...metadataParams,
        externalDocumentationUrl: value,
      });
    },
  );

  const save = useCallback(() => {
    mutation.mutate({
      request: {
        setInstallPageMetadataRequestBody: {
          toolsetId: toolset.id,
          ...metadataParams,
        },
      },
    });
  }, [toolset, metadataParams, mutation]);

  return (
    <Stack
      className={cn(
        "gap-4 items-start",
        mutation.status === "pending" && "animate-pulse",
      )}
    >
      <Stack direction="horizontal" align="center" gap={2}>
        <CodeBlock
          copyable={toolset.mcpIsPublic}
        >{`${mcpUrl}/install`}</CodeBlock>
        <Link external to={`${mcpUrl}/install`} noIcon>
          <Button
            variant="secondary"
            className="px-4"
            disabled={!toolset.mcpIsPublic}
          >
            <Button.Text>View</Button.Text>
            <Button.RightIcon>
              <Icon name="external-link" className="w-4 h-4" />
            </Button.RightIcon>
          </Button>
        </Link>
      </Stack>
      <Heading> MCP Logo </Heading>
      <Type muted small className="max-w-2xl">
        The logo associated with this install page
      </Type>
      <div className="inline-block">
        <CompactUpload
          onUpload={handleUpload}
          renderFilePreview={() =>
            metadataParams.logoAssetId && (
              <AssetImage
                assetId={metadataParams.logoAssetId}
                className="w-16 h-16"
              />
            )
          }
        />
      </div>
      <Heading> Documentation Link </Heading>
      <Type muted small className="max-w-2xl">
        A link to your own MCP documentation that will be featured at the top of
        your install page
      </Type>
      <Input
        type="text"
        placeholder="https://my-documentation.link"
        className="w-full"
        {...urlInputHandlers}
      />
      <Stack direction={"horizontal"} gap={2}>
        <Button
          onClick={save}
          disabled={urlInputHandlers.error || !shouldUpdate(metadataParams, result.data?.metadata)}
        >
          <Button.Text>Save</Button.Text>{" "}
        </Button>
        <Button
          variant="secondary"
          disabled={!shouldUpdate(metadataParams, result.data?.metadata)}
          onClick={() => {
            setMetadataParams({
              logoAssetId: result.data?.metadata?.logoAssetId,
              externalDocumentationUrl:
                result.data?.metadata?.externalDocumentationUrl,
            });
          }}
        >
          <Button.Text>Discard</Button.Text>
        </Button>
      </Stack>
    </Stack>
  );
}
