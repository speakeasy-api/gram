import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSdkClient } from "@/contexts/Sdk";

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

/**
 * Hook for fetching feedback (votes + comments) for a corpus file.
 * TODO: Wire to real API when SDK hooks are generated.
 */
export function useFeedback(_filePath: string) {
  // Stub — will be implemented in GREEN phase
  return {
    data: undefined as FeedbackData | undefined,
    isLoading: false,
    vote: (_direction: VoteDirection) => {},
    isVoting: false,
  };
}

/**
 * Hook for fetching comments for a corpus file.
 * TODO: Wire to real API when SDK hooks are generated.
 */
export function useComments(_filePath: string) {
  // Stub — will be implemented in GREEN phase
  return {
    data: undefined as FeedbackComment[] | undefined,
    isLoading: false,
    addComment: (_content: string) => {},
    isAddingComment: false,
  };
}
