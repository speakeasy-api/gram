import type { Gram } from "@gram/client";
import type {
  CorpusFeedbackCommentResult,
  CorpusFeedbackResult,
} from "@gram/client/models/components";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
import {
  useCorpusAddCommentMutation,
  useCorpusVoteFeedbackMutation,
  useGramContext,
} from "@gram/client/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getFeedbackFixture } from "./feedback-fixtures";

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
  readOnly?: boolean;
};

export type VoteDirection = "up" | "down";

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

function shouldUseFeedbackFixture(error: unknown): boolean {
  return import.meta.env.DEV && isPermissionDenied(error);
}

export function feedbackQueryKey(filePath: string) {
  return ["corpus", "feedback", filePath] as const;
}

export function commentsQueryKey(filePath: string) {
  return ["corpus", "feedback", "comments", filePath] as const;
}

/**
 * Hook for fetching feedback (votes) for a corpus file.
 */
export function useFeedback(filePath: string) {
  const client = useGramContext() as Gram;
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: feedbackQueryKey(filePath),
    queryFn: async (): Promise<FeedbackData> => {
      try {
        const result: CorpusFeedbackResult = await client.corpus.getFeedback({
          getFeedbackRequestBody: {
            filePath,
          },
        });

        return {
          downvotes: result.downvotes,
          labels: result.labels,
          upvotes: result.upvotes,
          userVote: result.userVote ?? null,
        };
      } catch (error) {
        if (!shouldUseFeedbackFixture(error)) {
          throw error;
        }

        return {
          ...getFeedbackFixture(filePath).feedback,
          readOnly: true,
        };
      }
    },
    enabled: !!filePath,
    throwOnError: false,
  });

  const voteMutation = useCorpusVoteFeedbackMutation({
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: feedbackQueryKey(filePath),
      });
    },
  });

  return {
    data: query.data,
    isLoading: query.isLoading,
    vote: (direction: VoteDirection) =>
      query.data?.readOnly
        ? undefined
        : voteMutation.mutate({
            request: {
              voteFeedbackRequestBody: {
                filePath,
                direction,
              },
            },
          }),
    isReadOnly: query.data?.readOnly ?? false,
    error: query.error,
    isVoting: voteMutation.isPending,
  };
}

/**
 * Hook for fetching and adding comments on a corpus file.
 */
export function useComments(filePath: string) {
  const client = useGramContext() as Gram;
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: commentsQueryKey(filePath),
    queryFn: async (): Promise<FeedbackComment[]> => {
      try {
        const result = await client.corpus.listComments({
          getFeedbackRequestBody: {
            filePath,
          },
        });
        return result.comments.map((comment: CorpusFeedbackCommentResult) => ({
          author: comment.author,
          authorType: comment.authorType,
          content: comment.content,
          createdAt: comment.createdAt.toISOString(),
          downvotes: comment.downvotes,
          id: comment.id,
          upvotes: comment.upvotes,
        }));
      } catch (error) {
        if (!shouldUseFeedbackFixture(error)) {
          throw error;
        }

        return getFeedbackFixture(filePath).comments;
      }
    },
    enabled: !!filePath,
    throwOnError: false,
  });

  const addMutation = useCorpusAddCommentMutation({
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: commentsQueryKey(filePath),
      });
    },
  });

  return {
    data: query.data,
    isLoading: query.isLoading,
    addComment: (content: string) =>
      addMutation.mutate({
        request: {
          addCommentRequestBody: {
            filePath,
            content,
          },
        },
      }),
    error: query.error,
    isAddingComment: addMutation.isPending,
  };
}
