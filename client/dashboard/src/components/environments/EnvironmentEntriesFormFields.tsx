import { Input } from "@/components/ui/input";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  Environment,
  EnvironmentEntryInput as EnvironmentEntryInputType,
} from "@gram/client/models/components";
import { invalidateAllListEnvironments } from "@gram/client/react-query";
import {
  useMutation,
  UseMutationResult,
  useQueryClient,
} from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";

const SECRET_FIELD_INDICATORS = ["SECRET", "KEY"] as const;
const PASSWORD_MASK = "••••••••";

interface UseEnvironmentFormParams {
  environment: Environment | null;
  relevantEnvVars: string[];
}

export interface UseEnvironmentFormReturn {
  entries: EnvironmentEntryFormInput[];
  isDirty: boolean;
  mutation: UseMutationResult<Environment, Error, void>;
  cancel: () => void;
}

export interface EnvironmentEntryFormInput {
  varName: string;
  isSensitive: boolean;
  initialValue: string | null;
  inputValue: string;
  onValueChange: (value: string) => void;
}

export function useEnvironmentForm({
  environment,
  relevantEnvVars,
}: UseEnvironmentFormParams): UseEnvironmentFormReturn {
  const queryClient = useQueryClient();
  const sdkClient = useSdkClient();
  const telemetry = useTelemetry();

  const [environmentEntries, setEnvironmentEntries] = useState<
    EnvironmentEntryFormInput[]
  >([]);
  const [isDirty, setIsDirty] = useState(false);

  const handleValueChange = useCallback((varName: string, value: string) => {
    setEnvironmentEntries((prev) =>
      prev.map((entry) =>
        entry.varName === varName ? { ...entry, inputValue: value } : entry,
      ),
    );
  }, []);

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
          onValueChange: (value: string) => handleValueChange(varName, value),
        };
      },
    );

    setEnvironmentEntries(initialValues);
  }, [
    environment?.slug,
    environment?.entries,
    relevantEnvVars,
    handleValueChange,
  ]);

  useEffect(() => {
    setIsDirty(environmentEntries.some((entry) => entry.inputValue !== ""));
  }, [environmentEntries]);

  const mutation = useMutation<Environment, Error, void>({
    mutationFn: async (): Promise<Environment> => {
      if (!environment) {
        throw new Error("No environment selected");
      }

      const { slug: environmentSlug } = environment;

      const entriesToUpdate: EnvironmentEntryInputType[] = environmentEntries
        .filter((entry) => entry.inputValue.trim() !== "")
        .map((entry) => ({ name: entry.varName, value: entry.inputValue }));

      return await sdkClient.environments.updateBySlug({
        slug: environmentSlug,
        updateEnvironmentRequestBody: {
          entriesToUpdate,
          entriesToRemove: [],
        },
      });
    },
    onSuccess: () => {
      telemetry.capture("environment_event", {
        action: "environment_updated_from_toolset_auth",
      });

      setIsDirty(false);
    },
    onSettled: () => {
      invalidateAllListEnvironments(queryClient);
    },
  });

  const handleCancel = useCallback(() => {
    setEnvironmentEntries((prev) =>
      prev.map((entry) => ({ ...entry, inputValue: "" })),
    );
  }, []);

  return {
    entries: environmentEntries,
    isDirty,
    mutation,
    cancel: handleCancel,
  };
}

interface EnvironmentEntriesFormFieldsProps {
  entries: EnvironmentEntryFormInput[];
  relevantEnvVars: string[];
  disabled: boolean;
  onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
}

function EnvironmentEntryInput({
  entry,
  disabled,
  onKeyDown,
}: {
  entry: EnvironmentEntryFormInput;
  disabled: boolean;
  onKeyDown: (e: React.KeyboardEvent<HTMLInputElement>) => void;
}) {
  const [isFocused, setIsFocused] = useState(false);

  const hasExistingValue = entry.initialValue !== null;
  const entryIsDirty = entry.inputValue !== "";

  // Compute display value
  let displayValue = "";
  if (entryIsDirty) {
    displayValue = entry.inputValue;
  } else if (!isFocused && hasExistingValue && entry.initialValue) {
    displayValue = entry.isSensitive ? PASSWORD_MASK : entry.initialValue;
  }

  return (
    <div className="grid grid-cols-2 gap-4 items-center">
      <label
        htmlFor={`env-${entry.varName}`}
        className="text-sm font-medium text-foreground"
      >
        {entry.varName}
      </label>
      <Input
        id={`env-${entry.varName}`}
        value={displayValue}
        onChange={entry.onValueChange}
        onFocus={() => setIsFocused(true)}
        onBlur={() => setIsFocused(false)}
        onKeyDown={onKeyDown}
        placeholder={
          hasExistingValue ? "Replace existing value" : "Enter value"
        }
        type={entry.isSensitive ? "password" : "text"}
        className="font-mono text-sm"
        disabled={disabled}
      />
    </div>
  );
}

export function EnvironmentEntriesFormFields({
  entries,
  relevantEnvVars,
  disabled,
  onKeyDown,
}: EnvironmentEntriesFormFieldsProps) {
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
      {entries.map((entry) => (
        <EnvironmentEntryInput
          key={entry.varName}
          entry={entry}
          disabled={disabled}
          onKeyDown={onKeyDown}
        />
      ))}
    </div>
  );
}
