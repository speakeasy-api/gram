import { CodeBlock } from "@/components/code";
import { Dialog } from "@/components/ui/dialog";
import { Label as Heading } from "@/components/ui/label";
import { Link } from "@/components/ui/link";
import { Type } from "@/components/ui/type";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { Toolset } from "@/lib/toolTypes";
import type { McpMetadata } from "@gram/client/models/components";
import {
  invalidateGetMcpMetadata,
  useMcpMetadataSetMutation,
} from "@gram/client/react-query";
import { Button, cn, Icon, Input, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  type ChangeEventHandler,
  type JSX,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { AssetImage } from "../asset-image";
import { CompactUpload, useAssetImageUploadHandler } from "../upload";

interface ConfigFormProps {
  toolset: Toolset;
  form: UseMcpMetadataMetadataFormResult;
  isLoading: boolean;
}

interface MetadataParams {
  logoAssetId: string | undefined;
  externalDocumentationUrl: string | undefined;
  externalDocumentationText: string | undefined;
  instructions: string | undefined;
  installationOverrideUrl: string | undefined;
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

export interface UseMcpMetadataMetadataFormResult {
  valid: ValidationResult;
  dirty: boolean;
  brandingDirty: boolean;
  instructionsDirty: boolean;
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
  docsTextInputHandlers: {
    value: string | undefined;
    error?: boolean;
    onChange: ChangeEventHandler<HTMLInputElement>;
  };
  instructionsHandlers: {
    value: string | undefined;
    onChange: ChangeEventHandler<HTMLTextAreaElement>;
  };
  installationOverrideUrlInputHandlers: {
    value: string | undefined;
    error?: boolean;
    onChange: ChangeEventHandler<HTMLInputElement>;
  };
  reset: () => void;
  resetBranding: () => void;
  resetInstructions: () => void;
  save: () => void;
  saveAsync: () => Promise<void>;
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

export function useMcpMetadataMetadataForm(
  toolsetSlug: string,
  currentMetadata?: McpMetadata,
): UseMcpMetadataMetadataFormResult {
  const queryClient = useQueryClient();

  const [metadataParams, setMetadataParams] = useState<MetadataParams>({
    installationOverrideUrl:
      currentMetadata?.installationOverrideUrl ?? undefined,
    externalDocumentationUrl:
      currentMetadata?.externalDocumentationUrl ?? undefined,
    externalDocumentationText:
      currentMetadata?.externalDocumentationText ?? undefined,
    logoAssetId: currentMetadata?.logoAssetId ?? undefined,
    instructions: currentMetadata?.instructions ?? undefined,
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
        installationOverrideUrl: currentMetadata?.installationOverrideUrl,
        externalDocumentationUrl: currentMetadata?.externalDocumentationUrl,
        externalDocumentationText: currentMetadata?.externalDocumentationText,
        logoAssetId: currentMetadata?.logoAssetId,
        instructions: currentMetadata?.instructions,
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

  const brandingDirty = useMemo(() => {
    const brandingKeys = [
      "logoAssetId",
      "externalDocumentationUrl",
      "externalDocumentationText",
      "installationOverrideUrl",
    ] as const;
    if (!currentMetadata) {
      return brandingKeys.some((key) => metadataParams[key] !== undefined);
    }
    return brandingKeys.some(
      (key) => metadataParams[key] !== currentMetadata[key],
    );
  }, [currentMetadata, metadataParams]);

  const instructionsDirty = useMemo(() => {
    if (!currentMetadata) {
      return metadataParams.instructions !== undefined;
    }
    return metadataParams.instructions !== currentMetadata.instructions;
  }, [currentMetadata, metadataParams.instructions]);

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
      externalDocumentationText: currentMetadata?.externalDocumentationText,
      instructions: currentMetadata?.instructions,
      installationOverrideUrl: currentMetadata?.installationOverrideUrl,
    });
  }, [currentMetadata]);

  const resetBranding = useCallback(() => {
    setMetadataParams((prev) => ({
      ...prev,
      logoAssetId: currentMetadata?.logoAssetId,
      externalDocumentationUrl: currentMetadata?.externalDocumentationUrl,
      externalDocumentationText: currentMetadata?.externalDocumentationText,
      installationOverrideUrl: currentMetadata?.installationOverrideUrl,
    }));
  }, [currentMetadata]);

  const resetInstructions = useCallback(() => {
    setMetadataParams((prev) => ({
      ...prev,
      instructions: currentMetadata?.instructions,
    }));
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

  const saveAsync = useCallback(async () => {
    await mutation.mutateAsync({
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
    brandingDirty,
    instructionsDirty,
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
    docsTextInputHandlers: {
      value: metadataParams.externalDocumentationText ?? "",
      onChange: (e) =>
        setMetadataParams((prev) => ({
          ...prev,
          externalDocumentationText:
            e.target.value === "" ? undefined : e.target.value,
        })),
    },
    installationOverrideUrlInputHandlers: {
      value: metadataParams.installationOverrideUrl ?? "",
      onChange: (e) =>
        setMetadataParams((prev) => ({
          ...prev,
          installationOverrideUrl:
            e.target.value === "" ? undefined : e.target.value,
        })),
    },
    instructionsHandlers: {
      value: metadataParams.instructions ?? "",
      onChange: (e) =>
        setMetadataParams((prev) => ({
          ...prev,
          instructions: e.target.value === "" ? undefined : e.target.value,
        })),
    },
    reset,
    resetBranding,
    resetInstructions,
    save,
    saveAsync,
  };
}

export function InstallPageConfigForm({
  toolset,
  form,
  isLoading,
}: ConfigFormProps) {
  const { installPageUrl } = useMcpUrl(toolset);
  const [open, setOpen] = useState(false);

  if (!installPageUrl) {
    return null;
  }

  return (
    <div className="flex items-center gap-2 rounded-lg border bg-muted/20 p-2">
      <CodeBlock
        className="flex-grow overflow-hidden"
        innerClassName="!p-2 !pr-10 !bg-white dark:!bg-zinc-950"
        preClassName="whitespace-nowrap overflow-auto"
        copyable={true}
      >
        {installPageUrl}
      </CodeBlock>
      <Dialog
        open={open}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            form.resetBranding();
          }
          setOpen(nextOpen);
        }}
      >
        <Dialog.Trigger asChild>
          <Button variant="secondary">
            <Button.LeftIcon>
              <Icon name="palette" />
            </Button.LeftIcon>
            <Button.Text>Edit Branding</Button.Text>
          </Button>
        </Dialog.Trigger>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Install Page Configuration</Dialog.Title>
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
                className="w-full max-h-[200px]"
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
            <div>
              <Heading> Documentation Text </Heading>
              <Type muted small className="max-w-2xl">
                What your custom link will say on the MCP page
              </Type>
            </div>
            <div className="relative">
              <Input
                type="text"
                placeholder="View Docs"
                className="w-full"
                {...form.docsTextInputHandlers}
              />
              {form.valid.message && (
                <span className="absolute -bottom-4 left-0 text-xs text-destructive">
                  {form.valid.message}
                </span>
              )}
            </div>
            <div>
              <Heading> Installation Override URL </Heading>
              <Type muted small className="max-w-2xl">
                A URL to redirect to instead of the default installation page
                when someone navigates to your MCP URL in their browser.
              </Type>
            </div>
            <div className="relative">
              <Input
                type="text"
                placeholder="Leave unset to use the default installation page"
                className="w-full"
                {...form.installationOverrideUrlInputHandlers}
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
              disabled={!form.brandingDirty}
              onClick={form.resetBranding}
            >
              <Button.Text>Discard</Button.Text>
            </Button>
            <Button
              onClick={() => {
                form.save();
                setOpen(false);
              }}
              disabled={isLoading || !form.valid.valid || !form.brandingDirty}
            >
              <Button.Text>Save</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
      <Link external to={installPageUrl} noIcon>
        <Button variant="primary" className="px-4">
          <Button.LeftIcon>
            <Icon name="external-link" className="w-4 h-4" />
          </Button.LeftIcon>
          <Button.Text>View</Button.Text>
        </Button>
      </Link>
    </div>
  );
}
