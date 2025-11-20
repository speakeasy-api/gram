import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  Environment,
  EnvironmentEntryInput as EnvironmentEntryInputType,
} from "@gram/client/models/components";
import {
  invalidateAllListEnvironments,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";

export interface EnvironmentEntryInputProps {
  varName: string;
  isSensitive: boolean;
  inputValue: string;
  entryValue: string | null;
  hasExistingValue: boolean;
  isDirty: boolean;
  isSaving: boolean;
  onValueChange: (varName: string, value: string) => void;
  onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
}

const SECRET_FIELD_INDICATORS = ["SECRET", "KEY"] as const;

interface UseEnvironmentEntriesFormParams {
  environment: Environment | null;
  relevantEnvVars: string[];
}

interface UseEnvironmentEntriesFormReturn {
  entries: EnvironmentEntryFormInput[];
  getInputPropsForEntry: (
    entry: EnvironmentEntryFormInput,
  ) => EnvironmentEntryInputProps;
  isDirty: boolean;
  persist: () => Promise<void>;
  cancel: () => void;
}

interface PersistedEnvironmentEntry {
  kind: "persisted";
  varName: string;
  isSensitive: boolean;
  initialValue: string;
  inputValue: string;
}

interface NewEnvironmentEntry {
  kind: "new";
  varName: string;
  isSensitive: boolean;
  inputValue: string;
}

type EnvironmentEntryFormInput =
  | PersistedEnvironmentEntry
  | NewEnvironmentEntry;

export function useEnvironmentEntriesForm({
  environment,
  relevantEnvVars,
}: UseEnvironmentEntriesFormParams): UseEnvironmentEntriesFormReturn {
  const queryClient = useQueryClient();
  const sdkClient = useSdkClient();
  const telemetry = useTelemetry();

  const [environmentEntries, setEnvironmentEntries] = useState<
    EnvironmentEntryFormInput[]
  >([]);
  const [isDirty, setIsDirty] = useState(false);

  useEffect(() => {
    const initialValues: EnvironmentEntryFormInput[] = relevantEnvVars.map(
      (varName) => {
        const entry = environment?.entries?.find((e) => e.name === varName);
        const isSensitive = SECRET_FIELD_INDICATORS.some((indicator) =>
          varName.includes(indicator),
        );

        if (entry?.value != null && entry.value.trim() !== "") {
          return {
            kind: "persisted" as const,
            varName,
            isSensitive,
            initialValue: entry.value,
            inputValue: "",
          };
        } else {
          return {
            kind: "new" as const,
            varName,
            isSensitive,
            inputValue: "",
          };
        }
      },
    );

    setEnvironmentEntries(initialValues);
  }, [environment?.slug, environment?.entries, relevantEnvVars]);

  useEffect(() => {
    setIsDirty(environmentEntries.some((entry) => entry.inputValue !== ""));
  }, [environmentEntries]);

  const persist = useCallback(async () => {
    if (!isDirty || !environment) return;

    const { slug: environmentSlug } = environment;

    const entriesToUpdate: EnvironmentEntryInputType[] = environmentEntries
      .filter((entry) => entry.inputValue.trim() !== "")
      .map((entry) => ({ name: entry.varName, value: entry.inputValue }));

    try {
      await sdkClient.environments.updateBySlug({
        slug: environmentSlug,
        updateEnvironmentRequestBody: {
          entriesToUpdate,
          entriesToRemove: [],
        },
      });

      telemetry.capture("environment_event", {
        action: "environment_updated_from_toolset_auth",
      });

      setIsDirty(false);
    } finally {
      invalidateAllListEnvironments(queryClient);
    }
  }, [
    isDirty,
    environment,
    environmentEntries,
    sdkClient,
    telemetry,
    queryClient,
  ]);

  const handleValueChange = useCallback((varName: string, value: string) => {
    setEnvironmentEntries((prev) =>
      prev.map((entry) =>
        entry.varName === varName ? { ...entry, inputValue: value } : entry,
      ),
    );
  }, []);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (e.key === "Escape" && isDirty) {
        setEnvironmentEntries((prev) =>
          prev.map((entry) => ({ ...entry, inputValue: "" })),
        );
        e.currentTarget.blur();
      }
    },
    [isDirty],
  );

  const getInputPropsForEntry = useCallback(
    (entry: EnvironmentEntryFormInput): EnvironmentEntryInputProps => {
      return {
        varName: entry.varName,
        isSensitive: entry.isSensitive,
        inputValue: entry.inputValue,
        entryValue: entry.kind === "persisted" ? entry.initialValue : null,
        hasExistingValue: entry.kind === "persisted",
        isDirty: entry.inputValue !== "",
        isSaving: false,
        onValueChange: handleValueChange,
        onKeyDown: handleKeyDown,
      };
    },
    [handleValueChange, handleKeyDown],
  );

  const handleCancel = useCallback(() => {
    setEnvironmentEntries((prev) =>
      prev.map((entry) => ({ ...entry, inputValue: "" })),
    );
  }, []);

  return {
    entries: environmentEntries,
    getInputPropsForEntry,
    isDirty,
    persist,
    cancel: handleCancel,
  };
}

export type { EnvironmentEntryFormInput };
