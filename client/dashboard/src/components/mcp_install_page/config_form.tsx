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
  useMemo,
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

interface MetadataParams {
  logoAssetId: string | undefined;
  externalDocumentationUrl: string | undefined;
}

interface UseMcpInstallPageMetadataFormResult {
  valid: boolean;
  dirty: boolean;
  isLoading: boolean;
  metadataParams: MetadataParams;
  logoUploadHandlers: {
    onUpload: ReturnType<typeof useAssetImageUploadHandler>;
    renderFilePreview: () => JSX.Element | undefined;
  };
  urlInputHandlers: {
    value: string | undefined;
    error?: boolean;
    onChange: ChangeEventHandler<HTMLInputElement>;
  };
  reset: () => void;
  save: () => void;
}

/*This is better implemented by taking a slice of the server state and running
a true deep equals. But we don't seem to have a deep equality implementation
available, and so we opt to implement a highly specific version instead  */
function equalsServerState(
  params: MetadataParams,
  current: MCPInstallPageMetadata,
): boolean {
  return (Object.keys(params) as (keyof MetadataParams)[]).every((key) => {
    return current[key] === params[key];
  });
}

function useMcpInstallPageMetadataForm(
  toolsetSlug: string,
  currentMetadata?: MCPInstallPageMetadata,
): UseMcpInstallPageMetadataFormResult {
  const queryClient = useQueryClient();

  const [metadataParams, setMetadataParams] = useState<MetadataParams>({
    externalDocumentationUrl:
      currentMetadata?.externalDocumentationUrl ?? undefined,
    logoAssetId: currentMetadata?.logoAssetId ?? undefined,
  });

  const [urlValid, setUrlValid] = useState(true);

  const mutation = useMcpInstallPageSetMutation({
    onSettled: () => {
      invalidateGetInstallPageMetadata(queryClient, [
        { toolsetSlug },
      ]);
    },
  });

  useEffect(() => {
    if (
      currentMetadata &&
      !equalsServerState(metadataParams, currentMetadata)
    ) {
      setMetadataParams({
        externalDocumentationUrl: currentMetadata?.externalDocumentationUrl,
        logoAssetId: currentMetadata?.logoAssetId,
      });
    }
  }, [currentMetadata]);

  useEffect(() => {
    if (!metadataParams.externalDocumentationUrl) {
      setUrlValid(true);
      return;
    }
    try {
      new URL(metadataParams.externalDocumentationUrl);
      setUrlValid(true);
    } catch (err) {
      setUrlValid(false);
    }
  }, [metadataParams.externalDocumentationUrl]);

  const dirty = useMemo(() => {
    if (
      !currentMetadata &&
      Object.values(metadataParams).some((val) => val !== undefined)
    ) {
      return true;
    }

    if (
      currentMetadata &&
      !equalsServerState(metadataParams, currentMetadata)
    ) {
      return true;
    }

    return false;
  }, [currentMetadata, metadataParams]);

  const handleUpload = useAssetImageUploadHandler((assetResult) => {
    setMetadataParams((prev) => ({
      ...prev,
      logoAssetId: assetResult.asset.id,
    }));
  });

  const reset = useCallback(() => {
    setMetadataParams({
      logoAssetId: currentMetadata?.logoAssetId,
      externalDocumentationUrl: currentMetadata?.externalDocumentationUrl,
    });
  }, [currentMetadata]);

  const save = useCallback(() => {
    mutation.mutate({
      request: {
        setInstallPageMetadataRequestBody: {
          toolsetSlug,
          ...metadataParams,
        },
      },
    });
  }, [toolsetSlug, metadataParams, mutation]);

  return {
    valid: urlValid,
    dirty,
    isLoading: mutation.isPending,
    metadataParams,
    logoUploadHandlers: {
      onUpload: handleUpload,
      renderFilePreview: () =>
        metadataParams.logoAssetId ? (
          <AssetImage
            assetId={metadataParams.logoAssetId}
            className="w-16 h-16"
          />
        ) : undefined,
    },
    urlInputHandlers: {
      value: metadataParams.externalDocumentationUrl ?? '',
      error:
        metadataParams.externalDocumentationUrl &&
        metadataParams.externalDocumentationUrl.length > 0
          ? !urlValid
          : undefined,
      onChange: (e) =>
        setMetadataParams((prev) => ({
          ...prev,
          externalDocumentationUrl:
            e.target.value === "" ? undefined : e.target.value,
        })),
    },
    reset,
    save,
  };
}

export function ConfigForm({ toolset }: ConfigFormProps) {
  const { url: mcpUrl } = useMcpUrl(toolset);

  const result = useGetInstallPageMetadata(
    { toolsetSlug: toolset.slug },
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

  const form = useMcpInstallPageMetadataForm(toolset.slug, result.data?.metadata);
  const isLoading = result.isLoading || form.isLoading;

  return (
    <Stack
      className={cn(
        "gap-4 items-start",
        isLoading && "animate-pulse",
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
          onUpload={form.logoUploadHandlers.onUpload}
          renderFilePreview={form.logoUploadHandlers.renderFilePreview}
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
        {...form.urlInputHandlers}
      />
      <Stack direction={"horizontal"} gap={2}>
        <Button variant="tertiary" disabled={!form.dirty} onClick={form.reset}>
          <Button.Text>Discard</Button.Text>
        </Button>
        <Button
          onClick={form.save}
          disabled={isLoading || !form.valid || !form.dirty}
        >
          <Button.Text>Save</Button.Text>
        </Button>
      </Stack>
    </Stack>
  );
}
