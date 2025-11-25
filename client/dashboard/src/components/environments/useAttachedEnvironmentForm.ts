import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useEnvironments } from "@/pages/environments/Environments";
import {
  Environment,
  ToolsetEnvironmentLink,
} from "@gram/client/models/components";
import { useGetToolsetEnvironment } from "@gram/client/react-query";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useCallback, useEffect, useState } from "react";
import { useMutation, UseMutationResult } from "@tanstack/react-query";

interface UseAttachedEnvironmentFormParams {
  toolsetId: string;
  onEnvironmentChange?: (slug: string) => void;
}

interface UseAttachedEnvironmentFormReturn {
  selectedEnvironment: Environment | null;
  onEnvironmentSelectorChange: (slug: string) => void;
  isDirty: boolean;
  mutation: UseMutationResult<ToolsetEnvironmentLink | null, Error, void>;
  cancel: () => void;
  isLoading: boolean;
}

interface FormState {
  environment: Environment | null;
  serverEnvironment: Environment | null;
  dirty: boolean;
  stateInitialized: boolean;
}

interface UseFormStateReturn {
  formState: FormState;
  environmentChanged: (environment: Environment | null) => void;
  reset: (environment: Environment | null) => void;
  serverDataReceived: (environment: Environment | null) => void;
}

function useFormState(): UseFormStateReturn {
  const [formState, setFormState] = useState<FormState>({
    environment: null,
    serverEnvironment: null,
    dirty: false,
    stateInitialized: false,
  });

  const environmentChanged = useCallback((environment: Environment | null) => {
    setFormState((prev) => ({
      ...prev,
      environment,
      dirty: true,
    }));
  }, []);

  const reset = useCallback((environment: Environment | null) => {
    setFormState((prev) => ({
      ...prev,
      environment,
      serverEnvironment: environment,
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
        serverEnvironment: environment,
        stateInitialized: true,
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

  const { formState, environmentChanged, reset, serverDataReceived } =
    useFormState();
  const attachedEnvironmentQuery = useAttachedEnvironmentQuery(toolsetId);

  useEffect(() => {
    serverDataReceived(attachedEnvironmentQuery.data ?? null);
  }, [attachedEnvironmentQuery.data]);

  const mutation = useMutation<ToolsetEnvironmentLink | null, Error, void>({
    mutationFn: async (): Promise<ToolsetEnvironmentLink | null> => {
      if (formState.environment?.id) {
        return await sdkClient.environments.setToolsetLink({
          setToolsetEnvironmentLinkRequestBody: {
            toolsetId,
            environmentId: formState.environment.id,
          },
        });
      } else {
        await sdkClient.environments.deleteToolsetLink({
          toolsetId,
        });
        return null;
      }
    },
    onSuccess: () => {
      telemetry.capture("toolset_event", {
        action: formState.environment?.id
          ? "toolset_environment_attached"
          : "toolset_environment_detached",
      });
    },
    onSettled: async () => {
      reset(formState.environment);
      await attachedEnvironmentQuery.refetch();
    },
  });

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
    reset(attachedEnvironmentQuery.data ?? null);
  }, [attachedEnvironmentQuery.data, reset]);

  return {
    selectedEnvironment: formState.environment,
    onEnvironmentSelectorChange: handleEnvironmentSelectorChange,
    isDirty: formState.dirty,
    mutation,
    cancel: handleCancel,
    isLoading: attachedEnvironmentQuery.isLoading || mutation.isPending,
  };
}
