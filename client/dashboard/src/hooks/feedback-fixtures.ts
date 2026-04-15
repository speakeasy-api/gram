export type FeedbackFixture = {
  feedback: {
    upvotes: number;
    downvotes: number;
    labels: string[];
    userVote: "up" | "down" | null;
  };
  comments: Array<{
    id: string;
    author: string;
    authorType: "human" | "agent";
    content: string;
    createdAt: string;
    upvotes: number;
    downvotes: number;
  }>;
};

const DEFAULT_FEEDBACK_FIXTURE: FeedbackFixture = {
  feedback: {
    upvotes: 12,
    downvotes: 3,
    labels: ["helpful", "accurate"],
    userVote: null,
  },
  comments: [
    {
      id: "c1",
      author: "alice",
      authorType: "human",
      content: "Great documentation!",
      createdAt: "2026-04-01T10:00:00Z",
      upvotes: 5,
      downvotes: 0,
    },
    {
      id: "c2",
      author: "doc-bot",
      authorType: "agent",
      content: "Added cross-references to related pages.",
      createdAt: "2026-04-02T14:30:00Z",
      upvotes: 2,
      downvotes: 1,
    },
  ],
};

const FEEDBACK_FIXTURES_BY_PATH: Record<string, FeedbackFixture> = {
  "README.md": {
    feedback: {
      upvotes: 7,
      downvotes: 1,
      labels: ["clear", "starter"],
      userVote: null,
    },
    comments: [
      {
        id: "readme-1",
        author: "maria",
        authorType: "human",
        content: "This intro is enough to get moving locally.",
        createdAt: "2026-04-03T09:15:00Z",
        upvotes: 3,
        downvotes: 0,
      },
    ],
  },
  "docs/guide.md": DEFAULT_FEEDBACK_FIXTURE,
};

export function getFeedbackFixture(filePath: string): FeedbackFixture {
  const fixture =
    FEEDBACK_FIXTURES_BY_PATH[filePath] ?? DEFAULT_FEEDBACK_FIXTURE;

  return {
    feedback: {
      ...fixture.feedback,
      labels: [...fixture.feedback.labels],
    },
    comments: fixture.comments.map((comment) => ({ ...comment })),
  };
}
