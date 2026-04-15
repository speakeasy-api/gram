import type { Gram } from "@gram/client";
import type { CorpusAnnotationResult } from "@gram/client/models/components";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
import {
  useCorpusCreateAnnotationMutation,
  useGramContext,
} from "@gram/client/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";

export type Annotation = {
  id: string;
  author: string;
  authorType: "human" | "agent";
  content: string;
  createdAt: string;
};

type AnnotationQueryData = {
  annotations: Annotation[];
  readOnly: boolean;
};

export function annotationsQueryKey(filePath: string) {
  return ["corpus", "annotations", filePath] as const;
}

function isPermissionDenied(error: unknown): boolean {
  if (error instanceof ServiceError) {
    return error.statusCode === 403;
  }

  return (
    typeof error === "object" &&
    error !== null &&
    "statusCode" in error &&
    (error as { statusCode?: number }).statusCode === 403
  );
}

function shouldUseAnnotationFallback(error: unknown): boolean {
  return import.meta.env.DEV && isPermissionDenied(error);
}

/**
 * Hook for fetching and creating annotations on a corpus file.
 */
export function useAnnotations(filePath: string) {
  const client = useGramContext() as Gram;
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: annotationsQueryKey(filePath),
    queryFn: async (): Promise<AnnotationQueryData> => {
      try {
        const result = await client.corpus.listAnnotations({
          getFeedbackRequestBody: {
            filePath,
          },
        });
        return {
          annotations: result.annotations.map(
            (annotation: CorpusAnnotationResult) => ({
              author: annotation.author,
              authorType: annotation.authorType,
              content: annotation.content,
              createdAt: annotation.createdAt.toISOString(),
              id: annotation.id,
            }),
          ),
          readOnly: false,
        };
      } catch (error) {
        if (!shouldUseAnnotationFallback(error)) {
          throw error;
        }

        return {
          annotations: [],
          readOnly: true,
        };
      }
    },
    enabled: !!filePath,
    throwOnError: false,
  });

  const createMutation = useCorpusCreateAnnotationMutation({
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: annotationsQueryKey(filePath),
      });
    },
  });

  return {
    data: query.data?.annotations,
    isLoading: query.isLoading,
    create: (content: string) =>
      query.data?.readOnly
        ? undefined
        : createMutation.mutate({
            request: {
              createAnnotationRequestBody: {
                content,
                filePath,
              },
            },
          }),
    isReadOnly: query.data?.readOnly ?? false,
    error: query.error,
    isCreating: createMutation.isPending,
  };
}
