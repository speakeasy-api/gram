import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { useComments, useFeedback } from "@/hooks/useFeedback";
import { cn } from "@/lib/utils";
import { ThumbsDownIcon, ThumbsUpIcon } from "lucide-react";

/**
 * FeedbackPanel — displays vote counts, upvote/downvote buttons, and comment
 * thread for a corpus file. Fetches data via useFeedback + useComments hooks.
 */
export function FeedbackPanel({ filePath }: { filePath: string }) {
  const { data: feedback, vote } = useFeedback(filePath);
  const { data: comments } = useComments(filePath);

  if (!feedback) {
    return null;
  }

  return (
    <div className="border-t border-border">
      <VoteBar
        upvotes={feedback.upvotes}
        downvotes={feedback.downvotes}
        userVote={feedback.userVote}
        labels={feedback.labels}
        onUpvote={() => vote("up")}
        onDownvote={() => vote("down")}
      />
      {comments && comments.length > 0 && <CommentThread comments={comments} />}
    </div>
  );
}

function VoteBar({
  upvotes,
  downvotes,
  userVote,
  labels,
  onUpvote,
  onDownvote,
}: {
  upvotes: number;
  downvotes: number;
  userVote?: "up" | "down" | null;
  labels: string[];
  onUpvote: () => void;
  onDownvote: () => void;
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-2.5 border-b border-border">
      <div className="flex items-center gap-1.5">
        <button
          aria-label="Upvote"
          onClick={onUpvote}
          className={cn(
            "p-0.5 rounded transition-colors",
            userVote === "up"
              ? "text-primary"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          <ThumbsUpIcon className="h-3.5 w-3.5" />
        </button>
        <span className="text-xs font-bold tabular-nums">{upvotes}</span>
      </div>
      <div className="flex items-center gap-1.5">
        <button
          aria-label="Downvote"
          onClick={onDownvote}
          className={cn(
            "p-0.5 rounded transition-colors",
            userVote === "down"
              ? "text-destructive"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          <ThumbsDownIcon className="h-3.5 w-3.5" />
        </button>
        <span className="text-xs font-bold tabular-nums">{downvotes}</span>
      </div>
      {labels.length > 0 && (
        <div className="flex flex-wrap gap-1 ml-auto">
          {labels.map((label) => (
            <Badge key={label} variant="secondary" className="text-[10px]">
              {label}
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}

function CommentThread({
  comments,
}: {
  comments: Array<{
    id: string;
    author: string;
    authorType: "human" | "agent";
    content: string;
    createdAt: string;
    upvotes: number;
    downvotes: number;
  }>;
}) {
  return (
    <div className="divide-y divide-border">
      {comments.map((comment) => (
        <CommentRow key={comment.id} comment={comment} />
      ))}
    </div>
  );
}

function CommentRow({
  comment,
}: {
  comment: {
    id: string;
    author: string;
    authorType: "human" | "agent";
    content: string;
    createdAt: string;
    upvotes: number;
    downvotes: number;
  };
}) {
  return (
    <div className="flex gap-2.5 px-4 py-2.5">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5 text-xs mb-0.5">
          <span className="font-medium text-foreground">{comment.author}</span>
          {comment.authorType === "agent" && (
            <Badge variant="default" className="text-[10px] px-1 py-0">
              Agent
            </Badge>
          )}
        </div>
        <Type small className="text-foreground">
          {comment.content}
        </Type>
      </div>
    </div>
  );
}
