import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  Environment,
  EnvironmentEntryInput as EnvironmentEntryInputType,
} from "@gram/client/models/components";
import { invalidateAllListEnvironments } from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import {
  EnvironmentEntryInput,
  EnvironmentEntryInputProps,
} from "./EnvironmentEntryInput";

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

interface EnvironmentEntryFormInput {
  varName: string;
  isSensitive: boolean;
  initialValue: string | null;
  inputValue: string;
}

function useEnvironmentEntriesForm({
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

        return {
          varName,
          isSensitive,
          initialValue:
            entry?.value != null && entry.value.trim() !== ""
              ? entry.value
              : null,
          inputValue: "",
        };
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
        entryValue: entry.initialValue,
        hasExistingValue: entry.initialValue !== null,
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

interface EnvironmentEntriesFormFieldsProps {
  environment: Environment | null;
  relevantEnvVars: string[];
}

export function EnvironmentEntriesFormFields({
  environment,
  relevantEnvVars,
}: EnvironmentEntriesFormFieldsProps) {
  const form = useEnvironmentEntriesForm({
    environment,
    relevantEnvVars,
  });

  if (relevantEnvVars.length === 0) {
    return (
      <div className="text-center py-8">
        <p className="text-sm text-muted-foreground">
          No authentication required for this toolset
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {form.entries.map((input) => (
        <EnvironmentEntryInput
          key={input.varName}
          {...form.getInputPropsForEntry(input)}
        />
      ))}
    </div>
  );
}

export function useEnvironmentEntriesFormActions(
  environment: Environment | null,
  relevantEnvVars: string[],
) {
  const form = useEnvironmentEntriesForm({
    environment,
    relevantEnvVars,
  });

  return {
    isDirty: form.isDirty,
    persist: form.persist,
    cancel: form.cancel,
  };
}
