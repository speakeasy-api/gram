import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { MultiSelectGroup } from "@/components/ui/MultiSelect";

/**
 * Options for an "Accessible by" filter: the org's people, with the viewer
 * pulled out of the list and named for what they are to the reader.
 *
 * "Accessible by me" is the question this filter is opened for most, and
 * finding your own name among forty others to ask it is the slow path.
 */
export function accessibleByFilterOptions(
  members: AccessMember[],
  currentUserId: string | undefined,
): MultiSelectGroup[] {
  const groups: MultiSelectGroup[] = [];
  const self = members.find((member) => member.id === currentUserId);
  if (self) {
    groups.push({
      heading: "You",
      options: [
        { value: self.id, label: "Current user", description: self.email },
      ],
    });
  }
  const others = members
    .filter((member) => member.id !== currentUserId)
    .map((member) => ({
      value: member.id,
      label: member.name || member.email,
      description: member.name ? member.email : undefined,
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
  if (others.length > 0) {
    groups.push({ heading: "Other users", options: others });
  }
  return groups;
}
