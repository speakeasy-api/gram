import { Label } from "@/components/ui/Label";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import type { JSX, ReactNode } from "react";
import { TOKEN_LIFETIMES } from "./device-enrollment";
import { useHooksProjects } from "./useHooksProjects";

/**
 * The two choices a bootstrap token needs: where the box's hooks go, and how
 * long the token may wait to be redeemed. Shared by the agent wizard and the
 * agent page so both ask the same question the same way.
 */
export function DeviceEnrollmentFields({
  projectId,
  onProjectChange,
  lifetimeSeconds,
  onLifetimeChange,
  hooksNote,
  disabled = false,
}: {
  projectId: string;
  onProjectChange: (projectId: string) => void;
  lifetimeSeconds: number;
  onLifetimeChange: (seconds: number) => void;
  /** Whether the agent accepts hooks for the chosen project, under the select. */
  hooksNote?: ReactNode;
  disabled?: boolean;
}): JSX.Element {
  const projects = useHooksProjects();
  return (
    <div className="max-w-xl space-y-6">
      <div className="space-y-2">
        <Label htmlFor="device-hooks-project">Hooks project</Label>
        <Select
          value={projectId}
          onValueChange={onProjectChange}
          disabled={disabled || projects.length === 0}
        >
          <SelectTrigger id="device-hooks-project" aria-label="Hooks project">
            <SelectValue placeholder="Choose a project" />
          </SelectTrigger>
          <SelectContent>
            {projects.map((project) => (
              <SelectItem key={project.id} value={project.id}>
                {project.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Text muted small>
          Where this box sends its AI tools&rsquo; hook events. They are
          accepted only while the agent is allowed to send hooks to this
          project.
        </Text>
        {hooksNote}
      </div>
      <div className="space-y-2">
        <Label id="device-token-lifetime">Bootstrap token expires after</Label>
        <SegmentedControl
          ariaLabel="Bootstrap token lifetime"
          value={String(lifetimeSeconds)}
          onChange={(value) => onLifetimeChange(Number(value))}
          disabled={disabled}
          options={TOKEN_LIFETIMES.map((option) => ({
            value: String(option.seconds),
            label: option.label,
          }))}
        />
        <Text muted small>
          The token enrolls one box, once. Run the install before it expires.
        </Text>
      </div>
    </div>
  );
}
