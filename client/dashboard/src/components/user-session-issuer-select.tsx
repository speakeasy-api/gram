import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { PROJECT_SPECIFIC_ISSUER_VALUE } from "./user-session-issuer-select.utils";

export function UserSessionIssuerSelect({
  issuers,
  value,
  onValueChange,
  includeProjectSpecific = false,
  disabled = false,
  ariaLabel = "User session issuer",
}: {
  issuers: UserSessionIssuer[];
  value: string;
  onValueChange: (value: string) => void;
  includeProjectSpecific?: boolean;
  disabled?: boolean;
  ariaLabel?: string;
}): JSX.Element {
  const labelCounts = new Map<string, number>();
  for (const issuer of issuers) {
    const ownership = issuer.projectId === "" ? "Organization" : "Project";
    const label = `${issuer.slug} — ${ownership}`;
    labelCounts.set(label, (labelCounts.get(label) ?? 0) + 1);
  }

  return (
    <Select value={value} onValueChange={onValueChange} disabled={disabled}>
      <SelectTrigger aria-label={ariaLabel} className="w-full max-w-md">
        <SelectValue placeholder="Select a user session issuer" />
      </SelectTrigger>
      <SelectContent>
        {includeProjectSpecific ? (
          <SelectItem value={PROJECT_SPECIFIC_ISSUER_VALUE}>
            Create a project-specific issuer
          </SelectItem>
        ) : null}
        {issuers.map((issuer) => {
          const ownership =
            issuer.projectId === "" ? "Organization" : "Project";
          const label = `${issuer.slug} — ${ownership}`;
          const disambiguator =
            (labelCounts.get(label) ?? 0) > 1
              ? ` · ${issuer.id.slice(0, 8)}`
              : "";

          return (
            <SelectItem key={issuer.id} value={issuer.id}>
              {label}
              {disambiguator}
            </SelectItem>
          );
        })}
      </SelectContent>
    </Select>
  );
}
