import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useEnvironments } from "@/pages/environments/Environments";
import { Environment } from "@gram/client/models/components";
import { useGetToolsetEnvironment } from "@gram/client/react-query";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useCallback, useEffect, useState } from "react";

interface UseAttachedEnvironmentFormParams {
  toolsetId: string;
  onEnvironmentChange?: (slug: string) => void;
}

interface UseAttachedEnvironmentFormReturn {
  selectedEnvironment: Environment | null;
  onEnvironmentSelectorChange: (slug: string) => void;
  isDirty: boolean;
  persist: () => Promise<void>;
  cancel: () => void;
  isLoading: boolean;
}

interface FormState {
  dirty: boolean;
  environment: Environment | null;
  stateInitialized: boolean;
  serverEnvironmentId: string | undefined;
}

interface UseFormStateReturn {
  formState: FormState;
  environmentChanged: (environment: Environment | null) => void;
  reset: () => void;
  serverDataReceived: (environment: Environment | null) => void;
}

function useFormState(): UseFormStateReturn {
  const [formState, setFormState] = useState<FormState>({
    dirty: false,
    environment: null,
    stateInitialized: false,
    serverEnvironmentId: undefined,
  });

  const environmentChanged = useCallback((environment: Environment | null) => {
    setFormState((prev) => ({
      ...prev,
      environment,
      dirty: true,
    }));
  }, []);

  const reset = useCallback(() => {
    setFormState((prev) => ({
      ...prev,
      dirty: false,
    }));
  }, []);

  const serverDataReceived = useCallback((environment: Environment | null) => {
    setFormState((prev) => {
      // Only sync if not dirty
      if (prev.dirty) {
        return prev;
      }

      return {
        ...prev,
        environment,
        stateInitialized: true,
        serverEnvironmentId: environment?.id,
        dirty: prev.stateInitialized && prev.environment?.id !== environment?.id,
      };
    });
  }, []);

  return {
    formState,
    environmentChanged,
    reset,
    serverDataReceived,
  };
}

function useAttachedEnvironmentQuery(toolsetId: string) {
  return useGetToolsetEnvironment(
    {
      toolsetId,
    },
    undefined,
    {
      retry: (_, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return true;
      },
      throwOnError: false,
    },
  );
}

export function useAttachedEnvironmentForm({
  toolsetId,
  onEnvironmentChange,
}: UseAttachedEnvironmentFormParams): UseAttachedEnvironmentFormReturn {
  const environments = useEnvironments();
  const sdkClient = useSdkClient();
  const telemetry = useTelemetry();

  const { formState, environmentChanged, reset, serverDataReceived } = useFormState();
  const attachedEnvironmentQuery = useAttachedEnvironmentQuery(toolsetId);

  // Sync from server data
  useEffect(() => {
    serverDataReceived(attachedEnvironmentQuery.data ?? null);
  }, [attachedEnvironmentQuery.data, serverDataReceived]);

  const persist = useCallback(async () => {
    if (!formState.dirty) return;

    try {
      if (formState.environment?.id) {
        await sdkClient.environments.setToolsetLink({
          setToolsetEnvironmentLinkRequestBody: {
            toolsetId,
            environmentId: formState.environment.id,
          },
        });
      } else {
        await sdkClient.environments.deleteToolsetLink({
          toolsetId,
        });
      }

      telemetry.capture("toolset_event", {
        action: formState.environment?.id
          ? "toolset_environment_attached"
          : "toolset_environment_detached",
      });

      reset();
    } finally {
      await attachedEnvironmentQuery.refetch();
    }
  }, [
    formState.dirty,
    formState.environment,
    sdkClient,
    toolsetId,
    telemetry,
    reset,
    attachedEnvironmentQuery,
  ]);

  const handleEnvironmentSelectorChange = useCallback(
    (slug: string) => {
      const selectedEnv = environments.find((env) => env.slug === slug);
      environmentChanged(selectedEnv ?? null);
      if (onEnvironmentChange) {
        onEnvironmentChange(slug);
      }
    },
    [environments, onEnvironmentChange, environmentChanged],
  );

  const handleCancel = useCallback(() => {
    serverDataReceived(attachedEnvironmentQuery.data ?? null);
  }, [attachedEnvironmentQuery.data, serverDataReceived]);

  return {
    selectedEnvironment: formState.environment,
    onEnvironmentSelectorChange: handleEnvironmentSelectorChange,
    isDirty: formState.dirty,
    persist,
    cancel: handleCancel,
    isLoading: attachedEnvironmentQuery.isLoading,
  };
}
