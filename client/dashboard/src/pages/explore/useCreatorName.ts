import { useMembers } from "@gram/client/react-query/members.js";

/** Who saved a widget, by the project's members, as a display name. */
export function useCreatorName(): (userId: string | undefined) => string {
  const { data, isPending } = useMembers();
  return (userId) => {
    if (!userId) return "Unknown";
    const member = (data?.members ?? []).find((m) => m.id === userId);
    if (member) return member.name || member.email;
    // Members still loading, or a creator who has since left (or the
    // members could not be fetched).
    return isPending ? "…" : "A former member";
  };
}
