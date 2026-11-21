import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { handleError } from "@/lib/errors";
import { useOrgRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import {
  invalidateAllMcpFeedbackSettings,
  setMcpFeedbackSettingsData,
  useMcpFeedbackSettings,
} from "@gram/client/react-query/mcpFeedbackSettings.js";
import { useUpdateMcpFeedbackSettingsMutation } from "@gram/client/react-query/updateMcpFeedbackSettings.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  CustomizeFeedbackToolSheet,
  type FeedbackToolCustomization,
} from "./CustomizeFeedbackToolSheet";
import { FeedbackReports } from "./FeedbackReports";
import { FeedbackToolPanel } from "./FeedbackToolPanel";

/**
 * Where a server's owner hears from the agents using it: one switch adds a
 * feedback tool to the server with nothing to fill in, and what agents send
 * through it is collected below.
 */
export function FeedbackTab({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const { hasScope, hasAnyScope } = useRBAC();
  // A hosted server's access is granted on its toolset, as the API checks it.
  const canEdit = hasScope("mcp:write", mcpServer.toolsetId ?? mcpServer.id);
  // Exports and webhooks are organization pages not everyone can open.
  const canOpenOrganizationPages = hasAnyScope(
    ["org:read", "org:admin"],
    organization.id,
  );

  const settingsQuery = useMcpFeedbackSettings(
    { mcpServerId: mcpServer.id },
    undefined,
    { throwOnError: false },
  );
  const settings = settingsQuery.data;
  const update = useUpdateMcpFeedbackSettingsMutation();
  const [customizing, setCustomizing] = useState(false);

  // The API replaces the stored settings whole, so each change resends the
  // rest, along with the version it was made from: a change someone else made
  // in the meantime is refused rather than silently undone.
  const save = async (
    next: FeedbackToolCustomization & { enabled: boolean },
    messages: { saved: string; failed: string },
  ): Promise<"saved" | "failed" | "conflict"> => {
    try {
      const saved = await update.mutateAsync({
        request: {
          updateMcpFeedbackSettingsForm: {
            mcpServerId: mcpServer.id,
            ...next,
            expectedUpdatedAt: settings?.updatedAt,
          },
        },
      });
      setMcpFeedbackSettingsData(
        queryClient,
        [{ mcpServerId: mcpServer.id }],
        saved,
      );
      await invalidateAllMcpFeedbackSettings(queryClient);
      toast.success(messages.saved);
      return "saved";
    } catch (error) {
      if (error instanceof GramError && error.statusCode === 409) {
        await invalidateAllMcpFeedbackSettings(queryClient);
        toast.error(messages.failed, {
          description:
            "Someone else changed these settings since you opened them. Their change is shown now; make yours again if it is still needed.",
        });
        return "conflict";
      }
      handleError(error, { title: messages.failed });
      return "failed";
    }
  };

  if (settingsQuery.isPending) {
    return <Skeleton className="h-24 w-full" />;
  }
  if (!settings) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load feedback for this server"
        description="Nothing was changed. Try again in a moment."
        action={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void settingsQuery.refetch()}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        }
      />
    );
  }

  // Reports can also leave this page; the hint sits with the reports, and only
  // for people who can open the organization pages it links to.
  const sendElsewhere =
    settings.enabled && canOpenOrganizationPages ? (
      <>
        You can also send reports to{" "}
        <Link
          to={orgRoutes.dataExports.href()}
          className="underline underline-offset-2"
        >
          your observability stack
        </Link>{" "}
        or to{" "}
        <Link
          to={orgRoutes.webhooks.href()}
          className="underline underline-offset-2"
        >
          a webhook
        </Link>
        .
      </>
    ) : undefined;

  return (
    <Stack gap={6}>
      <FeedbackToolPanel
        settings={settings}
        canEdit={canEdit}
        saving={update.isPending}
        onCustomize={() => setCustomizing(true)}
        onToggle={(enabled) =>
          void save(
            {
              enabled,
              toolName: settings.toolName,
              toolDescription: settings.toolDescription,
            },
            enabled
              ? {
                  saved: "Feedback turned on",
                  failed: "Couldn't turn on feedback",
                }
              : {
                  saved: "Feedback turned off. Reports are kept.",
                  failed: "Couldn't turn off feedback",
                },
          )
        }
      />

      {/* A server nobody set this up on cannot have reports to load. */}
      {(settings.enabled || settings.updatedAt) && (
        <FeedbackReports
          mcpServerId={mcpServer.id}
          feedbackIsOn={settings.enabled}
          sendElsewhere={sendElsewhere}
        />
      )}

      {customizing && (
        <CustomizeFeedbackToolSheet
          settings={settings}
          open
          saving={update.isPending}
          onOpenChange={setCustomizing}
          onSave={(customization) =>
            void save(
              { enabled: settings.enabled, ...customization },
              {
                saved: "Feedback tool updated",
                failed: "Couldn't save the feedback tool",
              },
            ).then((outcome) => {
              // After a conflict the sheet would still show the old settings.
              if (outcome !== "failed") setCustomizing(false);
            })
          }
        />
      )}
    </Stack>
  );
}
