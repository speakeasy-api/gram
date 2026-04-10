import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";

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
 * TODO: Wire to real API when SDK hooks are generated.
 */
export function useAnnotations(_filePath: string) {
  // Stub — will be implemented in GREEN phase
  return {
    data: undefined as Annotation[] | undefined,
    isLoading: false,
    create: (_content: string) => {},
    isCreating: false,
  };
}
