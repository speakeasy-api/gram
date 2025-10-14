import type { McpMetadata } from "@gram/client/models/components";
import {
  useGetMcpMetadata,
  useMcpMetadataSetMutation,
  invalidateGetMcpMetadata,
} from "@gram/client/react-query";
import { Button, Stack, Input, cn, Icon } from "@speakeasy-api/moonshine";
import { Link } from "@/components/ui/link";
import { CompactUpload, useAssetImageUploadHandler } from "../upload";
import { Label as Heading } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import {
  type ChangeEventHandler,
  type JSX,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { AssetImage } from "../asset-image";
import { useQueryClient } from "@tanstack/react-query";
import { CodeBlock } from "@/components/code";
import { useMcpUrl } from "@/pages/mcp/MCPDetails";
import { Dialog } from "@/components/ui/dialog";
import { Toolset } from "@/lib/toolTypes";
import { GramError } from "@gram/client/models/errors/gramerror.js";

interface ConfigFormProps {
  toolset: Toolset;
}

interface MetadataParams {
  logoAssetId: string | undefined;
  externalDocumentationUrl: string | undefined;
}

type ValidationResult =
  | {
      valid: true;
      message?: undefined;
    }
  | {
      valid: false;
      message: string;
    };

interface UseMcpMetadataMetadataFormResult {
  valid: ValidationResult;
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
  current: McpMetadata,
): boolean {
  return (Object.keys(params) as (keyof MetadataParams)[]).every((key) => {
    return current[key] === params[key];
  });
}

function useMcpMetadataMetadataForm(
  toolsetSlug: string,
  currentMetadata?: McpMetadata,
): UseMcpMetadataMetadataFormResult {
  const queryClient = useQueryClient();

  const [metadataParams, setMetadataParams] = useState<MetadataParams>({
    externalDocumentationUrl:
      currentMetadata?.externalDocumentationUrl ?? undefined,
    logoAssetId: currentMetadata?.logoAssetId ?? undefined,
  });

  const [urlValid, setUrlValid] = useState<ValidationResult>({ valid: true });

  const mutation = useMcpMetadataSetMutation({
    onSettled: () => {
      invalidateGetMcpMetadata(queryClient, [{ toolsetSlug }]);
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
      setUrlValid({ valid: true });
      return;
    }

    const value = metadataParams.externalDocumentationUrl;

    if (!value.startsWith("https://") && !value.startsWith("http://")) {
      setUrlValid({
        valid: false,
        message: "URLs should start with https://",
      });
      return;
    }

    const parsedUrl = URL.parse(value);

    if (!parsedUrl) {
      setUrlValid({
        valid: false,
        message: "Invalid URL format",
      });
    } else {
      setUrlValid({ valid: true });
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
        setMcpMetadataRequestBody: {
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
      value: metadataParams.externalDocumentationUrl ?? "",
      error:
        metadataParams.externalDocumentationUrl &&
        metadataParams.externalDocumentationUrl.length > 0
          ? !urlValid.valid
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
  const [open, setOpen] = useState(false);

  const result = useGetMcpMetadata({ toolsetSlug: toolset.slug }, undefined, {
    retry: (_, err) => {
      if (err instanceof GramError && err.statusCode === 404) {
        return false;
      }
      return true;
    },
    throwOnError: false,
  });

  const form = useMcpMetadataMetadataForm(toolset.slug, result.data?.metadata);
  const isLoading = result.isLoading || form.isLoading;

  return (
    <Stack direction="vertical" justify="space-between" align="start" gap={2}>
      <CodeBlock
        copyable={toolset.mcpIsPublic}
      >{`${mcpUrl}/install`}</CodeBlock>
      <Stack direction="horizontal" gap={2}>
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
        <Dialog open={open} onOpenChange={setOpen}>
          <Dialog.Trigger asChild>
            <Button variant="tertiary">
              <Button.Text>
                <Icon name="settings" />
              </Button.Text>
            </Button>
          </Dialog.Trigger>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Install Page Configuration</Dialog.Title>
              <Dialog.Description>
                Customize your MCP install page
              </Dialog.Description>
            </Dialog.Header>
            <Stack className={cn("gap-4", isLoading && "animate-pulse")}>
              <div>
                <Heading> MCP Logo </Heading>
                <Type muted small className="max-w-2xl">
                  The logo presented on this page
                </Type>
              </div>
              <div className="inline-block">
                <CompactUpload
                  allowedExtensions={["png", "jpg", "jpeg"]}
                  onUpload={form.logoUploadHandlers.onUpload}
                  renderFilePreview={form.logoUploadHandlers.renderFilePreview}
                />
              </div>
              <div>
                <Heading> Documentation Link </Heading>
                <Type muted small className="max-w-2xl">
                  A link to your own MCP documentation that will be featured at
                  the top of the page
                </Type>
              </div>
              <div className="relative">
                <Input
                  type="text"
                  placeholder="https://my-documentation.link"
                  className="w-full"
                  {...form.urlInputHandlers}
                />
                {form.valid.message && (
                  <span className="absolute -bottom-4 left-0 text-xs text-destructive">
                    {form.valid.message}
                  </span>
                )}
              </div>
            </Stack>
            <Dialog.Footer>
              <Button
                variant="tertiary"
                disabled={!form.dirty}
                onClick={form.reset}
              >
                <Button.Text>Discard</Button.Text>
              </Button>
              <Button
                onClick={() => {
                  form.save();
                  setOpen(false);
                }}
                disabled={isLoading || !form.valid.valid || !form.dirty}
              >
                <Button.Text>Save</Button.Text>
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      </Stack>
    </Stack>
  );
}
