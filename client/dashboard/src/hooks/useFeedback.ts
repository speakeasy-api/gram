import { getServerURL } from "@/lib/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

export type FeedbackComment = {
  id: string;
  author: string;
  authorType: "human" | "agent";
  content: string;
  createdAt: string;
  upvotes: number;
  downvotes: number;
};

export type FeedbackData = {
  upvotes: number;
  downvotes: number;
  labels: string[];
  userVote?: "up" | "down" | null;
  comments: FeedbackComment[];
};

export type VoteDirection = "up" | "down";

export function feedbackQueryKey(filePath: string) {
  return ["corpus", "feedback", filePath] as const;
}

export function commentsQueryKey(filePath: string) {
  return ["corpus", "feedback", "comments", filePath] as const;
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
 * Hook for fetching feedback (votes) for a corpus file.
 */
export function useFeedback(filePath: string) {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: feedbackQueryKey(filePath),
    queryFn: () => rpc<FeedbackData>("corpus.getFeedback", { filePath }),
    enabled: !!filePath,
  });

  const voteMutation = useMutation({
    mutationFn: (direction: VoteDirection) =>
      rpc("corpus.voteFeedback", { filePath, direction }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: feedbackQueryKey(filePath) });
    },
  });

  return {
    data: query.data,
    isLoading: query.isLoading,
    vote: (direction: VoteDirection) => voteMutation.mutate(direction),
    isVoting: voteMutation.isPending,
  };
}

/**
 * Hook for fetching and adding comments on a corpus file.
 */
export function useComments(filePath: string) {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: commentsQueryKey(filePath),
    queryFn: async () => {
      const result = await rpc<{ comments: FeedbackComment[] }>(
        "corpus.listComments",
        { filePath },
      );
      return result.comments;
    },
    enabled: !!filePath,
  });

  const addMutation = useMutation({
    mutationFn: (content: string) =>
      rpc("corpus.addComment", { filePath, content }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: commentsQueryKey(filePath) });
    },
  });

  return {
    data: query.data,
    isLoading: query.isLoading,
    addComment: (content: string) => addMutation.mutate(content),
    isAddingComment: addMutation.isPending,
  };
}
