import {
  DetailFrame,
  DetailHeaderLayout,
  DetailHeaderTitle,
  DetailHeaderTitleSkeleton,
} from "@/components/detail/detail-frame";
import { ErrorAlert } from "@/components/ui/Alert";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { useDistributionPlugin } from "@gram/client/react-query/distributionPlugin.js";
import { useSkillsInfinite } from "@gram/client/react-query/skills.js";
import { ArrowLeft } from "lucide-react";
import { Link, useLocation, useParams, useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  PLUGIN_SKILLS_SECTION_ID,
  pluginSectionHref,
} from "./plugin-detail-sections";
import { PluginSkillsSection } from "./PluginSkillsSection";

/** No organization configuration, audience, membership or publishing queries. */
export function PluginDistributionDetail(): JSX.Element {
  const { pluginId } = useParams<{ pluginId: string }>();
  const [params] = useSearchParams();
  const location = useLocation();
  const routes = useRoutes();
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
  const isLoading =
    (!requestedSkillId && skills.isPending) || (!!skillId && plugin.isPending);
  return (
    <DetailFrame
      breadcrumbs={{
        substitutions: { [pluginId ?? ""]: plugin.data?.name ?? "Plugin" },
      }}
      tabsLabel="Plugin sections"
      tabs={[
        {
          key: PLUGIN_SKILLS_SECTION_ID,
          title: "Skills",
          // Keeps the concrete skill this reader is authorized through.
          href: `${pluginSectionHref(routes, pluginId!, PLUGIN_SKILLS_SECTION_ID)}${location.search}`,
        },
      ]}
      activeTab={PLUGIN_SKILLS_SECTION_ID}
      tabsAside={
        requestedSkillId ? (
          <Link
            to={routes.skills.detail.href(requestedSkillId)}
            className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-sm transition-colors hover:no-underline"
          >
            <ArrowLeft className="size-3.5 shrink-0" aria-hidden />
            Back to skill
          </Link>
        ) : undefined
      }
      header={
        <DetailHeaderLayout
          title={
            plugin.data ? (
              <DetailHeaderTitle
                eyebrow="Distribute · Plugin"
                name={plugin.data.name}
                below={
                  plugin.data.description ? (
                    <Text muted>{plugin.data.description}</Text>
                  ) : undefined
                }
              />
            ) : isLoading ? (
              <DetailHeaderTitleSkeleton icon={false} />
            ) : null
          }
        />
      }
    >
      {error ? (
        <ErrorAlert error={error} />
      ) : isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : plugin.data ? (
        <PluginSkillsSection
          pluginId={plugin.data.id}
          skillId={skillId}
          onMutated={(message) => {
            toast.success(message);
          }}
        />
      ) : (
        <Text muted>No readable skills are available in this project.</Text>
      )}
    </DetailFrame>
  );
}
