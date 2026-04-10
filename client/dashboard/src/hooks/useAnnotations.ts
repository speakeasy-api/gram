import { getServerURL } from "@/lib/utils";
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

async function rpc<T>(
  method: string,
  body: Record<string, unknown>,
): Promise<T> {
  const res = await fetch(`${getServerURL()}/rpc/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`RPC ${method} failed: ${res.status}`);
  }
  return res.json() as Promise<T>;
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
