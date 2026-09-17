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
}: {
  issuers: UserSessionIssuer[];
  value: string;
  onValueChange: (value: string) => void;
  includeProjectSpecific?: boolean;
  disabled?: boolean;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onValueChange} disabled={disabled}>
      <SelectTrigger className="w-full max-w-md">
        <SelectValue placeholder="Select a user session issuer" />
      </SelectTrigger>
      <SelectContent>
        {includeProjectSpecific ? (
          <SelectItem value={PROJECT_SPECIFIC_ISSUER_VALUE}>
            Create a project-specific issuer
          </SelectItem>
        ) : null}
        {issuers.map((issuer) => (
          <SelectItem key={issuer.id} value={issuer.id}>
            {issuer.slug} —{" "}
            {issuer.projectId === "" ? "Organization" : "Project"}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
