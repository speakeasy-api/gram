import { useSlugs } from "@/contexts/Sdk";
import { useServerAssistantTransport } from "@/hooks/useServerAssistantTransport";
import type { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import {
  type Pinned,
  SortBy,
  SortOrder,
} from "@gram/client/models/operations/listchats";
import { useListChats } from "@gram/client/react-query/listChats.js";

// Resolve the project's Project Assistant and list its conversations. Shared
// by the pinned, recents, and compact sections so the assistant-resolution
// logic lives in one place. Pass a pinned state to filter, or `pinnedFirst` to
// list every chat with pinned ones ahead of the rest.
export function useProjectAssistantChats(
  filter: { pinned: Pinned } | { pinnedFirst: true; limit: number },
): {
  chats: ChatOverview[];
  loading: boolean;
} {
  const { projectSlug } = useSlugs();
  // Reuse the dock's managed-assistant resolution to scope the list to this
  // project's Project Assistant conversations.
  const { assistantId, ready } = useServerAssistantTransport(
    projectSlug ?? "",
    true,
  );
  const enabled = Boolean(ready && assistantId);
  const { data, isPending } = useListChats(
    {
      assistantId: assistantId || undefined,
      // Mirror the Elements runtime request: hide onboarding/warmup threads
      // from the recents lists.
      excludeSourceKind: "setup",
      sortBy: SortBy.LastMessageTimestamp,
      sortOrder: SortOrder.Desc,
      limit: 50,
      ...filter,
    },
    undefined,
    { enabled, throwOnError: false },
  );
  // A disabled query (no resolvable assistant) is never "loading" — report it
  // as settled-empty so the UI shows the empty state instead of spinning
  // forever.
  return { chats: data?.chats ?? [], loading: enabled && isPending };
}
