import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { buildLoginRedirectURL } from "@/lib/utils";
import { useRiskGetBlock } from "@gram/client/react-query/riskGetBlock.js";
import { useRiskSubmitBlockFeedbackMutation } from "@gram/client/react-query/riskSubmitBlockFeedback.js";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { ThumbsDown, ThumbsUp } from "lucide-react";
import { useEffect } from "react";
import { Navigate, useParams } from "react-router";

type Sentiment = "up" | "down";

/**
 * BlockDetailPage renders the durable tool call block page inside the app shell
 * (sidebar + header), at /:orgSlug/projects/:projectSlug/blocks/:id. Navigating
 * here from the dashboard is plain SPA routing, so there's no full reload.
 */
export function BlockDetailPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Title>Blocked tool call</Page.Header.Title>
        </Page.Header>
        <Page.Body>
          <BlockBody id={id} />
        </Page.Body>
      </Page>
    </RequireScope>
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

  const center = (node: JSX.Element) => (
    <div className="flex w-full justify-center py-12">{node}</div>
  );

  if (!id) {
    return center(
      <Type muted>This block link is missing its identifier.</Type>,
    );
  }
  if (isLoading) {
    return center(
      <Stack direction="horizontal" gap={2} align="center">
        <Icon name="loader-circle" className="size-4 animate-spin" />
        <Type muted small>
          Loading block…
        </Type>
      </Stack>,
    );
  }
  if (error || !block) {
    return center(
      <Type muted className="max-w-md text-center">
        We couldn't load this block. It may have been removed, or you may not
        have access to it in your current organization.
      </Type>,
    );
  }

  const onVote = async (sentiment: Sentiment) => {
    await submitFeedback({
      request: { submitRiskBlockFeedbackRequestBody: { id, sentiment } },
    });
    await refetch();
  };

  return (
    <div className="flex w-full justify-center py-12">
      <Stack gap={6} align="center" className="w-full max-w-xl">
        <Stack gap={3} align="center">
          <div className="bg-destructive/10 flex size-11 items-center justify-center rounded-full">
            <Icon name="shield" className="text-destructive size-5" />
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

        <div className="bg-muted/40 w-full rounded-md border p-4">
          <Type small className="whitespace-pre-wrap">
            {block.reason}
          </Type>
        </div>

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
    </div>
  );
}

/**
 * BlockRedirect resolves the slug-free durable URL embedded in an agent's block
 * message (/blocks/:id) into the in-app, chrome'd page under the owning
 * project. Keeping the agent link slug-free means the hook doesn't have to
 * resolve org/project slugs; this thin client hop maps the block's project to
 * its slug and routes there.
 */
export function BlockRedirect(): JSX.Element {
  const session = useSession();
  const { id } = useParams<{ id: string }>();

  useEffect(() => {
    if (!session.session) {
      window.location.href = buildLoginRedirectURL(window.location.pathname);
    }
  }, [session.session]);

  const { data: block, error } = useRiskGetBlock({ id: id ?? "" }, undefined, {
    enabled: !!id && !!session.session,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const centered = (node: JSX.Element) => (
    <div className="bg-background flex min-h-screen w-full items-center justify-center p-8">
      {node}
    </div>
  );

  if (!session.session) {
    return centered(
      <Stack direction="horizontal" gap={2} align="center">
        <Icon name="loader-circle" className="size-4 animate-spin" />
        <Type muted small>
          Redirecting to sign in…
        </Type>
      </Stack>,
    );
  }
  if (error) {
    return centered(
      <Type muted small>
        Block not found, or not accessible in your current organization.
      </Type>,
    );
  }
  if (!block) {
    return centered(
      <Stack direction="horizontal" gap={2} align="center">
        <Icon name="loader-circle" className="size-4 animate-spin" />
        <Type muted small>
          Loading block…
        </Type>
      </Stack>,
    );
  }

  const orgSlug = session.organization.slug;
  const projectSlug =
    session.organization.projects.find((p) => p.id === block.projectId)?.slug ??
    session.organization.projects[0]?.slug;

  if (!orgSlug || !projectSlug) {
    return centered(
      <Type muted small>
        Block not found, or not accessible in your current organization.
      </Type>,
    );
  }

  return (
    <Navigate replace to={`/${orgSlug}/projects/${projectSlug}/blocks/${id}`} />
  );
}

export default BlockDetailPage;
