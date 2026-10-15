import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import type { Selector } from "@gram/client/models/components/selector.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useState, type JSX } from "react";

export function AgentOwnerRulePicker({
  selectors,
  onChange,
}: {
  selectors: Selector[] | null;
  onChange: (selectors: Selector[] | null) => void;
}): JSX.Element {
  const members = useMembers();
  const [search, setSearch] = useState("");
  let mode = "selected";
  if (selectors === null || selectors.some((s) => s.resourceId === "*"))
    mode = "all";
  else if (selectors.length === 1 && selectors[0]?.resourceId === "self")
    mode = "self";
  const selected = new Set(selectors?.map((s) => s.resourceId) ?? []);
  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange(
      [...next].map((resourceId) => ({
        resourceKind: "agent_owner",
        resourceId,
      })),
    );
  };
  const visible = (members.data?.members ?? []).filter((member) =>
    `${member.name} ${member.email}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  return (
    <div className="flex flex-col gap-3">
      <Select
        value={mode}
        onValueChange={(value) => {
          if (value === "all") onChange(null);
          else if (value === "self")
            onChange([{ resourceKind: "agent_owner", resourceId: "self" }]);
          else onChange([]);
        }}
      >
        <SelectTrigger aria-label="Who new agents may belong to">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="self">Each role holder</SelectItem>
          <SelectItem value="selected">Selected people</SelectItem>
          <SelectItem value="all">Anyone in the organization</SelectItem>
        </SelectContent>
      </Select>
      <Text muted small>
        Only controls new agent creation. People keep management access to
        agents they already own.
      </Text>
      {mode === "selected" && (
        <>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={selected.has("self")}
              onCheckedChange={() => toggle("self")}
            />
            Also allow each role holder to create their own agents
          </label>
          <Input
            aria-label="Find owners"
            placeholder="Find people…"
            value={search}
            onChange={setSearch}
          />
          {members.isPending && (
            <Text muted small>
              Loading people…
            </Text>
          )}
          {members.isError && (
            <div role="alert">
              <Text small>Unable to load people.</Text>
              <Button variant="tertiary" onClick={() => void members.refetch()}>
                Retry
              </Button>
            </div>
          )}
          <div className="flex max-h-64 flex-col gap-2 overflow-y-auto">
            {visible.map((member) => (
              <label
                key={member.id}
                className="flex min-w-0 items-center gap-2 text-sm"
              >
                <Checkbox
                  checked={selected.has(member.id)}
                  onCheckedChange={() => toggle(member.id)}
                />
                <span className="truncate" title={member.name || member.email}>
                  {member.name || member.email}
                </span>
              </label>
            ))}
          </div>
          {!members.isPending && !members.isError && visible.length === 0 && (
            <Text muted small>
              No matching people.
            </Text>
          )}
        </>
      )}
    </div>
  );
}
