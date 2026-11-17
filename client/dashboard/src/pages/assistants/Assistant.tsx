import { useLocation, useParams } from "react-router";
import { useAssistantsGet } from "@gram/client/react-query/assistantsGet.js";
import { useRecentLabelOverride } from "@/components/command-palette/recentlyVisited";
import { useProject } from "@/contexts/Auth";
import { AssistantDetail } from "./detail/AssistantDetail";

export default function AssistantPage(): JSX.Element {
  useRecordAssistantRecent();
  return <AssistantDetail />;
}

// Register the assistant's name as this page's command-palette "Recently
// Visited" label. Without it, the visit is recorded by App from the URL alone,
// which for this id-keyed route falls back to the section title.
function useRecordAssistantRecent(): void {
  const { assistantId = "" } = useParams();
  const { pathname } = useLocation();
  const project = useProject();
  const { data } = useAssistantsGet(
    { id: assistantId, gramProject: project.slug },
    undefined,
    {
      enabled: Boolean(assistantId),
      retry: false,
      throwOnError: false,
      refetchOnWindowFocus: false,
    },
  );
  useRecentLabelOverride(pathname, data?.name);
}
