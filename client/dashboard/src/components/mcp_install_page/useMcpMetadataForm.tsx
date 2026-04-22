import type { McpMetadata } from "@gram/client/models/components";
import {
  invalidateGetMcpMetadata,
  useMcpMetadataSetMutation,
} from "@gram/client/react-query";
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
import { useAssetImageUploadHandler } from "@/components/useAssetImageUploadHandler";

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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- syncs server→local only on server change; including metadataParams would overwrite user edits
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
        toolsetSlug,
        ...metadataParams,
      },
    });
  }, [toolsetSlug, metadataParams, mutation]);

  const saveAsync = useCallback(async () => {
    await mutation.mutateAsync({
      request: {
        toolsetSlug,
        ...metadataParams,
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
            className="h-16 w-16"
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
