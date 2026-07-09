import { GramLogo } from "@/components/gram-logo";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { buildLoginRedirectURL } from "@/lib/utils";
import { useRiskGetBlock } from "@gram/client/react-query/riskGetBlock.js";
import { useRiskSubmitBlockFeedbackMutation } from "@gram/client/react-query/riskSubmitBlockFeedback.js";
import { Button, Stack } from "@/components/ui/moonshine";
import { LoaderCircle, Shield, ThumbsDown, ThumbsUp } from "lucide-react";
import { useEffect } from "react";
import { useParams } from "react-router";

type Sentiment = "up" | "down";

/**
 * BlockPage is the standalone durable tool call block page served at
 * /blocks/:id, deliberately rendered OUTSIDE the dashboard shell (no sidebar /
 * header) so it can be opened directly from the slug-free link an agent embeds
 * in its block message. It requires a Gram session but NOT org-admin: the
 * person whose agent was blocked is usually a regular org member, and the
 * backend scopes access to their active organization.
 */
export function BlockPage(): JSX.Element {
  const session = useSession();
  const { id } = useParams<{ id: string }>();

  useEffect(() => {
    if (!session.session) {
      window.location.href = buildLoginRedirectURL(window.location.pathname);
    }
  }, [session.session]);

  return (
    <div className="bg-background flex min-h-screen w-full flex-col items-center justify-center p-8">
      <Stack gap={8} align="center" className="w-full max-w-xl">
        <GramLogo className="w-25" variant="vertical" />
        {session.session ? (
          <BlockBody id={id} />
        ) : (
          <Stack direction="horizontal" gap={2} align="center">
            <LoaderCircle className="size-4 animate-spin" />
            <Type muted small>
              Redirecting to sign in…
            </Type>
          </Stack>
        )}
      </Stack>
    </div>
  );
}

function BlockBody({ id }: { id: string | undefined }) {
  const {
    data: block,
    isLoading,
    error,
    refetch,
  } = useRiskGetBlock({ id: id ?? "" }, undefined, {
    enabled: !!id,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const { mutateAsync: submitFeedback, isPending: isSubmitting } =
    useRiskSubmitBlockFeedbackMutation();

  if (!id) {
    return <Type muted>This block link is missing its identifier.</Type>;
  }
  if (isLoading) {
    return (
      <Stack direction="horizontal" gap={2} align="center">
        <LoaderCircle className="size-4 animate-spin" />
        <Type muted small>
          Loading block…
        </Type>
      </Stack>
    );
  }
  if (error || !block) {
    return (
      <Type muted className="max-w-md text-center">
        We couldn't load this block. It may have been removed, or you may not
        have access to it in your current organization.
      </Type>
    );
  }

  const onVote = async (sentiment: Sentiment) => {
    await submitFeedback({
      request: { submitRiskBlockFeedbackRequestBody: { id, sentiment } },
    });
    await refetch();
  };

  return (
    <Stack gap={6} align="center" className="w-full">
      <Stack gap={3} align="center">
        <div className="bg-destructive/10 flex size-11 items-center justify-center rounded-full">
          <Shield className="text-destructive size-5" />
        </div>
        <Stack gap={1} align="center">
          <Type variant="subheading" className="text-center">
            Tool call blocked
          </Type>
          <Type muted small className="text-center">
            Blocked by policy “{block.policyName}”
            {block.toolName ? ` · tool ${block.toolName}` : ""}
          </Type>
        </Stack>
      </Stack>

      {block.reason ? (
        <div className="bg-muted/40 w-full rounded-md border p-4">
          <Type small className="whitespace-pre-wrap text-center">
            {block.reason}
          </Type>
        </div>
      ) : null}

      <Stack gap={2} align="center">
        <Type muted small className="text-center">
          Was this block helpful?
        </Type>
        <Stack direction="horizontal" gap={2} align="center">
          <Button
            variant={block.feedback === "up" ? "secondary" : "tertiary"}
            disabled={isSubmitting}
            onClick={() => void onVote("up")}
          >
            <Button.LeftIcon>
              <ThumbsUp className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Helpful</Button.Text>
          </Button>
          <Button
            variant={block.feedback === "down" ? "secondary" : "tertiary"}
            disabled={isSubmitting}
            onClick={() => void onVote("down")}
          >
            <Button.LeftIcon>
              <ThumbsDown className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Not helpful</Button.Text>
          </Button>
        </Stack>
        {block.feedback ? (
          <Type muted small className="text-center">
            Thanks for the feedback.
          </Type>
        ) : null}
      </Stack>
    </Stack>
  );
}
