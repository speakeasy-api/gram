import { FullScreenPage } from "@/components/full-screen-page";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { buildLoginRedirectURL } from "@/lib/utils";
import { formatPlatform } from "@/lib/formatPlatform";
import { useRoutes } from "@/routes";
import { useRiskGetBlock } from "@gram/client/react-query/riskGetBlock.js";
import { useRiskSubmitBlockFeedbackMutation } from "@gram/client/react-query/riskSubmitBlockFeedback.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { ThumbsDown, ThumbsUp } from "lucide-react";
import { type MouseEvent, useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import { toast } from "sonner";

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
    <FullScreenPage contentClassName="max-w-xl">
      {session.session ? (
        <BlockBody id={id} />
      ) : (
        <Stack direction="horizontal" gap={2} align="center">
          <Icon name="loader-circle" className="size-4 animate-spin" />
          <Text muted small>
            Redirecting to sign in…
          </Text>
        </Stack>
      )}
    </FullScreenPage>
  );
}

function BlockBody({ id }: { id: string | undefined }) {
  const session = useSession();
  const client = useSdkClient();
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
  const [isSwitchingOrganization, setIsSwitchingOrganization] = useState(false);

  const organization = session.organizations.find((candidate) =>
    candidate.projects.some((project) => project.id === block?.projectId),
  );
  const project = organization?.projects.find(
    (candidate) => candidate.id === block?.projectId,
  );
  const routes = useRoutes({
    orgSlug: organization?.slug,
    projectSlug: project?.slug,
  });

  if (!id) {
    return <Text muted>This block link is missing its identifier.</Text>;
  }
  if (isLoading) {
    return (
      <Stack direction="horizontal" gap={2} align="center">
        <Icon name="loader-circle" className="size-4 animate-spin" />
        <Text muted small>
          Loading block…
        </Text>
      </Stack>
    );
  }
  if (error || !block) {
    return (
      <Text muted className="max-w-md text-center">
        We couldn't load this block. It may have been removed, or you may not
        have access to it in your current organization.
      </Text>
    );
  }

  const onVote = async (sentiment: Sentiment) => {
    await submitFeedback({
      request: { submitRiskBlockFeedbackRequestBody: { id, sentiment } },
    });
    await refetch();
  };

  const onRiskEventsClick = async (event: MouseEvent<HTMLAnchorElement>) => {
    if (!organization || organization.id === session.activeOrganizationId) {
      return;
    }

    event.preventDefault();
    if (isSwitchingOrganization) {
      return;
    }

    setIsSwitchingOrganization(true);
    try {
      await client.auth.switchScopes({ organizationId: organization.id });
      window.location.assign(routes.riskEvents.href());
    } catch {
      setIsSwitchingOrganization(false);
      toast.error("Unable to switch organizations. Please try again.");
    }
  };

  return (
    <Stack gap={6} align="center" className="w-full">
      <Stack gap={3} align="center">
        <Stack gap={1} align="center">
          <Text variant="subheading" className="text-center">
            Tool call blocked
          </Text>
          <Text muted small className="text-center">
            {/* Spend-rule blocks carry no risk policy; the rule name lives in
                the reason text below, so avoid a `policy ""` headline. */}
            {block.policyName
              ? `Blocked by policy “${block.policyName}”`
              : "Blocked by a Speakeasy spend rule"}
            {block.toolName ? ` · tool ${block.toolName}` : ""}
            {block.provider ? ` · via ${formatPlatform(block.provider)}` : ""}
          </Text>
        </Stack>
      </Stack>

      {block.reason ? (
        <div className="bg-muted/40 w-full border p-4">
          <Text small className="whitespace-pre-wrap text-center">
            {block.reason}
          </Text>
        </div>
      ) : null}

      {organization && project ? (
        <Link
          to={routes.riskEvents.href()}
          onClick={(event) => void onRiskEventsClick(event)}
          aria-disabled={isSwitchingOrganization}
          className="text-muted-foreground hover:text-foreground text-sm underline underline-offset-4 transition-colors"
        >
          View risk event log
        </Link>
      ) : null}

      <Stack gap={2} align="center">
        <Text muted small className="text-center">
          Was this block helpful?
        </Text>
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
          <Text muted small className="text-center">
            Thanks for the feedback.
          </Text>
        ) : null}
      </Stack>
    </Stack>
  );
}
