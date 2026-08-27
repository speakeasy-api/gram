import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import type { OtelDestination } from "@gram/client/models/components/oteldestination.js";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";

const CREATE_DESTINATION_VALUE = "__create_destination__";

type RouteDestinationItem = DropdownItem & {
  destination?: OtelDestination;
  createNew?: boolean;
};

export function RouteEditorSheet({
  destinations,
  routedDestinationIDs,
  saving,
  onClose,
  onCreate,
  onCreateDestination,
}: {
  destinations: OtelDestination[];
  routedDestinationIDs: Set<string>;
  saving: boolean;
  onClose: () => void;
  onCreate: (destination: OtelDestination, enabled: boolean) => Promise<void>;
  onCreateDestination: () => void;
}): JSX.Element {
  const [selectedID, setSelectedID] = useState<string>();
  const [enabled, setEnabled] = useState(true);
  const items = useMemo<RouteDestinationItem[]>(
    () => [
      ...destinations.map((destination) => {
        const alreadyRouted = routedDestinationIDs.has(destination.id);
        return {
          value: destination.id,
          label: destination.name,
          description: alreadyRouted
            ? "Already routed"
            : destination.endpointUrl,
          keywords: [destination.endpointUrl],
          disabled: alreadyRouted,
          destination,
        };
      }),
      {
        value: CREATE_DESTINATION_VALUE,
        label: "Create a new destination",
        icon: <Plus className="size-4" />,
        createNew: true,
      },
    ],
    [destinations, routedDestinationIDs],
  );
  const selected = items.find((item) => item.value === selectedID);

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="w-[520px] max-w-[calc(100vw-2rem)] gap-0 bg-card p-0 sm:max-w-[520px]"
      >
        <SheetHeader className="border-b px-6 py-5">
          <SheetTitle>New route</SheetTitle>
          <SheetDescription>
            Pick what leaves this project and where it goes.
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-6">
          <div className="space-y-3 py-6">
            <Label>Source</Label>
            <Select value="otel_forwarding" disabled>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="otel_forwarding">
                  Product telemetry
                </SelectItem>
              </SelectContent>
            </Select>
            <Text muted className="text-sm leading-relaxed">
              OTLP traces and logs from every MCP server and tool call. Risk
              findings, agent sessions and tool calls will appear here as they
              ship.
            </Text>
          </div>

          <div className="space-y-3 border-t py-6">
            <Label>Destination</Label>
            <Combobox
              items={items}
              selected={selected}
              onSelectionChange={(item) => {
                if (item.createNew) onCreateDestination();
                else setSelectedID(item.value);
              }}
              variant="secondary"
              className="h-10 w-full"
              contentClassName="w-[440px]"
              searchable
              searchPlaceholder="Search destinations"
            >
              {selected?.destination?.name ?? "Select a destination"}
            </Combobox>
          </div>

          <div className="flex items-start justify-between gap-4 border-t py-6">
            <div className="space-y-1">
              <Label>Start delivering</Label>
              <Text muted className="text-sm">
                Turn off to create the route paused.
              </Text>
            </div>
            <RequireScope scope="project:write" level="component">
              <Switch
                checked={enabled}
                onCheckedChange={setEnabled}
                aria-label="Start delivering"
              />
            </RequireScope>
          </div>
        </div>

        <SheetFooter className="min-h-14 flex-row items-center justify-between border-t bg-muted/30 px-6 py-3">
          <Text muted className="text-sm">
            Sensitive data follows the destination.
          </Text>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={onClose}
            >
              Cancel
            </Button>
            <RequireScope scope="project:write" level="component">
              <Button
                type="button"
                variant="primary"
                size="sm"
                disabled={!selected?.destination || saving}
                onClick={() => {
                  if (selected?.destination) {
                    void onCreate(selected.destination, enabled);
                  }
                }}
              >
                {saving ? "Creating" : "Create route"}
              </Button>
            </RequireScope>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
