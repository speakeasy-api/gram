import { rpc } from "@/lib/rpc";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

export type Annotation = {
  id: string;
  author: string;
  authorType: "human" | "agent";
  content: string;
  createdAt: string;
};

export function annotationsQueryKey(filePath: string) {
  return ["corpus", "annotations", filePath] as const;
}

/**
 * Hook for fetching and creating annotations on a corpus file.
 */
export function useAnnotations(filePath: string) {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: annotationsQueryKey(filePath),
    queryFn: async () => {
      const result = await rpc<{ annotations: Annotation[] }>(
        "corpus.listAnnotations",
        { filePath },
      );
      return result.annotations;
    },
    enabled: !!filePath,
  });

  const createMutation = useMutation({
    mutationFn: (content: string) =>
      rpc<Annotation>("corpus.createAnnotation", { filePath, content }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: annotationsQueryKey(filePath),
      });
    },
  });

  return {
    data: query.data,
    isLoading: query.isLoading,
    create: (content: string) => createMutation.mutate(content),
    isCreating: createMutation.isPending,
  };
}
