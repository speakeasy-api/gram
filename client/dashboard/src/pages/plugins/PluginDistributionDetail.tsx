import { Page } from "@/components/page-layout";
import { ErrorAlert } from "@/components/ui/Alert";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useDistributionPlugin } from "@gram/client/react-query/distributionPlugin.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { useParams, useSearchParams } from "react-router";
import { toast } from "sonner";
import { PluginSkillsSection } from "./PluginSkillsSection";

/** No organization configuration, audience, membership or publishing queries. */
export function PluginDistributionDetail(): JSX.Element {
  const { pluginId } = useParams<{ pluginId: string }>();
  const [params] = useSearchParams();
  const requestedSkillId = params.get("skillId");
  // Discovery is authorized against a concrete readable skill, not org:read.
  const skills = useSkillsInfinite({ limit: 1 }, undefined, {
    throwOnError: false,
    enabled: !requestedSkillId,
  });
  const skillId =
    requestedSkillId ?? skills.data?.pages[0]?.result.skills[0]?.id;
  const plugin = useDistributionPlugin(
    { id: pluginId!, skillId: skillId ?? "" },
    undefined,
    { enabled: !!skillId && !!pluginId, throwOnError: false },
  );
  const error = (!requestedSkillId && skills.error) || plugin.error;
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [pluginId ?? ""]: plugin.data?.name ?? "Plugin" }}
        />
      </Page.Header>
      <Page.Body fullWidth>
        <div className="mx-auto w-full max-w-[1270px] space-y-6 px-8 py-8">
          {error ? (
            <ErrorAlert error={error} />
          ) : (!requestedSkillId && skills.isPending) ||
            (skillId && plugin.isPending) ? (
            <Skeleton className="h-32 w-full" />
          ) : plugin.data ? (
            <>
              <Text as="h1" variant="subheading">
                {plugin.data.name}
              </Text>
              {plugin.data.description && (
                <Text muted>{plugin.data.description}</Text>
              )}
              <PluginSkillsSection
                pluginId={plugin.data.id}
                skillId={skillId}
                onMutated={(message) => toast.success(message)}
              />
            </>
          ) : (
            <Text muted>No readable skills are available in this project.</Text>
          )}
        </div>
      </Page.Body>
    </Page>
  );
}
