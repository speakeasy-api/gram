import type {
  InstructionToolMode,
  McpMetadata,
} from "@gram/client/models/components/mcpmetadata.js";
import { invalidateGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { useMcpMetadataSetMutation } from "@gram/client/react-query/mcpMetadataSet.js";
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
  instructionToolMode: InstructionToolMode | undefined;
  installationOverrideUrl: string | undefined;
}

// McpMetadataFormTarget tells the form which backend the metadata belongs to.
// Exactly one shape applies per render. Choosing this over a flat
// `(toolsetSlug?, mcpServerId?)` API forces every call site to spell out the
// backend, which keeps the mutation payload and React Query invalidation key
// in sync without runtime checks.
export type McpMetadataFormTarget =
  | { kind: "toolset"; toolsetSlug: string }
  | { kind: "mcp_server"; mcpServerId: string };

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
  instructionToolModeHandlers: {
    value: InstructionToolMode;
    onChange: (mode: InstructionToolMode) => void;
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
  target: McpMetadataFormTarget,
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
    instructionToolMode: currentMetadata?.instructionToolMode ?? undefined,
  });

  const [urlValid, setUrlValid] = useState<ValidationResult>({ valid: true });

  // Mutation request and the React Query invalidation key carry exactly the
  // same backend identifier so a refetch after save lands on the same row the
  // mutation touched. Keep these derived in one place to avoid drift, and
  // memo on the primitive backend id so save/saveAsync stay referentially
  // stable across renders.
  const targetKind = target.kind;
  const targetToolsetSlug =
    target.kind === "toolset" ? target.toolsetSlug : undefined;
  const targetMcpServerId =
    target.kind === "mcp_server" ? target.mcpServerId : undefined;
  const invalidationKey = useMemo(
    () =>
      targetKind === "toolset"
        ? { toolsetSlug: targetToolsetSlug! }
        : { mcpServerId: targetMcpServerId! },
    [targetKind, targetToolsetSlug, targetMcpServerId],
  );
  const backendRequestFields = useMemo(
    () =>
      targetKind === "toolset"
        ? { toolsetSlug: targetToolsetSlug! }
        : { mcpServerId: targetMcpServerId! },
    [targetKind, targetToolsetSlug, targetMcpServerId],
  );

  const mutation = useMcpMetadataSetMutation({
    onSettled: () => {
      void invalidateGetMcpMetadata(queryClient, [invalidationKey]);
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
        instructionToolMode: currentMetadata?.instructionToolMode,
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
      return (
        metadataParams.instructions !== undefined ||
        metadataParams.instructionToolMode !== undefined
      );
    }
    return (
      metadataParams.instructions !== currentMetadata.instructions ||
      metadataParams.instructionToolMode !== currentMetadata.instructionToolMode
    );
  }, [
    currentMetadata,
    metadataParams.instructions,
    metadataParams.instructionToolMode,
  ]);

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
      instructionToolMode: currentMetadata?.instructionToolMode,
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
      instructionToolMode: currentMetadata?.instructionToolMode,
    }));
  }, [currentMetadata]);

  const save = useCallback(() => {
    mutation.mutate({
      request: {
        setMcpMetadataRequestBody: {
          ...backendRequestFields,
          ...metadataParams,
        },
      },
    });
  }, [backendRequestFields, metadataParams, mutation]);

  const saveAsync = useCallback(async () => {
    await mutation.mutateAsync({
      request: {
        setMcpMetadataRequestBody: {
          ...backendRequestFields,
          ...metadataParams,
        },
      },
    });
  }, [backendRequestFields, metadataParams, mutation]);

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
    instructionToolModeHandlers: {
      value: metadataParams.instructionToolMode ?? "required",
      onChange: (mode: InstructionToolMode) =>
        setMetadataParams((prev) => ({
          ...prev,
          instructionToolMode: mode,
        })),
    },
    reset,
    resetBranding,
    resetInstructions,
    save,
    saveAsync,
  };
}
