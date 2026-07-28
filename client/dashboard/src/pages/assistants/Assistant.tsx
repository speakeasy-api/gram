import { useLocation, useParams } from "react-router";
import { useAssistantsGet } from "@gram/client/react-query/assistantsGet.js";
import { useRecentLabelOverride } from "@/components/command-palette/recentlyVisited";
import { EditAssistantOnboarding } from "./onboarding/AssistantOnboarding";

export default function AssistantPage(): JSX.Element {
  useRecordAssistantRecent();
  return <EditAssistantOnboarding />;
}

// Register the assistant's name as this page's command-palette "Recently
// Visited" label. Without it, the visit is recorded by App from the URL alone,
// which for this id-keyed route falls back to an opaque "Assistant · <id>".
function useRecordAssistantRecent(): void {
  const { assistantId = "" } = useParams();
  const { pathname } = useLocation();
  const { data } = useAssistantsGet({ id: assistantId }, undefined, {
    enabled: Boolean(assistantId),
    retry: false,
    throwOnError: false,
    refetchOnWindowFocus: false,
  });
  useRecentLabelOverride(pathname, data?.name);
}
