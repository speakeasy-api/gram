import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { type ServerGroup, serverLabel } from "./readableMcpServers";

export function McpServerSelect({
  id,
  groups,
  value,
  onChange,
  size,
  className,
  ariaLabel,
}: {
  id: string;
  groups: ServerGroup[];
  value: string;
  onChange: (value: string) => void;
  size?: "sm" | "default";
  className?: string;
  ariaLabel?: string;
}): JSX.Element {
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger
        id={id}
        size={size}
        className={className}
        aria-label={ariaLabel}
      >
        <SelectValue placeholder="Select an MCP server" />
      </SelectTrigger>
      <SelectContent>
        {groups.map((group) => (
          <SelectGroup key={group.projectId}>
            <SelectLabel>{group.projectName}</SelectLabel>
            {group.servers.map((server) => (
              <SelectItem key={server.id} value={server.id}>
                {serverLabel(server)}
              </SelectItem>
            ))}
          </SelectGroup>
        ))}
      </SelectContent>
    </Select>
  );
}
