import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  ToolSelectionPanel,
  type ToolAnnotation,
  type ToolSelectionServer,
  type ToolSelectionTool,
  type ToolSelectionToolRef,
} from "@/components/tool-selection/ToolSelectionPanel";
import { Check } from "lucide-react";
import { useState, type JSX } from "react";

/**
 * Narrows one rule to particular tools, or to tools carrying an annotation.
 *
 * The same panel the role editor uses, scoped to a single server: a rule here
 * and a rule there store the same selector, so they are chosen the same way.
 * Annotation values and stored dispositions are the same strings, so nothing
 * is translated on the way in or out.
 */
export function ToolNarrowingDialog({
  serverId,
  serverName,
  catalog,
  tools,
  dispositions,
  pending,
  onSave,
  onClose,
}: {
  serverId: string;
  serverName?: string;
  /** The server's tools, when this surface has them. */
  catalog?: ToolSelectionTool[];
  tools: string[];
  dispositions: string[];
  pending: boolean;
  onSave: (narrowing: { tools: string[]; dispositions: string[] }) => void;
  onClose: () => void;
}): JSX.Element {
  const [selectedTools, setSelectedTools] = useState<ToolSelectionToolRef[]>(
    () => tools.map((tool) => ({ serverId, toolName: tool })),
  );
  const [selectedAnnotations, setSelectedAnnotations] = useState<
    ToolAnnotation[]
  >(() => dispositions as ToolAnnotation[]);

  const servers: ToolSelectionServer[] = [
    {
      id: serverId,
      name: serverName ?? "This server",
      tools: catalog ?? [],
      status: catalog ? "ready" : "unavailable",
      // Remote servers resolve their tools per caller, so this surface may
      // have no catalogue to list; annotations still work there.
      unavailableLabel: "Tools are resolved when the server is called",
      emptyLabel: "No tools reported",
    },
  ];

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content className="flex max-h-[80vh] flex-col sm:max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>Narrow to tools</Dialog.Title>
          <Dialog.Description>
            Choose which of this server's tools the rule covers. An annotation
            keeps covering new tools as they are added; a list of names does
            not.
          </Dialog.Description>
        </Dialog.Header>

        <div className="min-h-0 flex-1 py-2">
          <ToolSelectionPanel
            servers={servers}
            mode={selectedTools.length > 0 ? "tools" : "annotations"}
            selectedAnnotations={selectedAnnotations}
            selectedTools={selectedTools}
            onSelectionChange={(change) => {
              setSelectedAnnotations([...change.annotations]);
              setSelectedTools([...change.tools]);
            }}
            className="h-[320px]"
          />
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <Button variant="secondary" onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={pending}
            onClick={() =>
              onSave({
                tools: selectedTools.map((tool) => tool.toolName),
                dispositions: selectedAnnotations,
              })
            }
          >
            <Button.LeftIcon>
              <Check className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Done</Button.Text>
          </Button>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
