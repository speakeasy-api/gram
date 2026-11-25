import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { useDetectorMode } from "@/pages/security/use-detector-mode";
import {
  ServerGuardrailsForm,
  type ToolsSource,
} from "@/pages/security/server-guardrails/ServerGuardrailsForm";
import {
  defaultServerGuardrailName,
  defaultServerGuardrailState,
  validateServerGuardrail,
  type ServerGuardrailState,
} from "@/pages/security/server-guardrails/server-guardrail-policy";
import { useCreateServerGuardrail } from "@/pages/security/server-guardrails/useCreateServerGuardrail";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerTools } from "@/pages/security/server-guardrails/useMcpServerTools";
import { useState } from "react";
import { toast } from "sonner";

/** Creates a guardrail scoped to one existing MCP server, with the tool picker
 *  populated from the server's discovered tools. */
export function AddServerGuardrailDialog({
  mcpServer,
  open,
  onOpenChange,
}: {
  mcpServer: McpServer;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const serverName = mcpServer.name ?? mcpServer.slug ?? "MCP server";
  const mode = useDetectorMode();
  const { tools, isLoading } = useMcpServerTools(mcpServer);
  const guardrail = useCreateServerGuardrail();
  const [state, setState] = useState<ServerGuardrailState>(
    defaultServerGuardrailState,
  );
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);

  const toolsSource: ToolsSource = isLoading
    ? { status: "loading" }
    : { status: "ready", tools };

  const close = (next: boolean) => {
    if (!next) {
      setState(defaultServerGuardrailState());
      setName("");
      setError(null);
    }
    onOpenChange(next);
  };

  const submit = async () => {
    const validation = validateServerGuardrail(state);
    if (!validation.ok) {
      setError(validation.message);
      return;
    }
    setError(null);
    try {
      await guardrail.create({
        state,
        mcpServerIds: [mcpServer.id],
        name: name.trim() || defaultServerGuardrailName(serverName),
      });
      toast.success("Guardrail created");
      close(false);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to create the guardrail",
      );
    }
  };

  return (
    <Dialog open={open} onOpenChange={close}>
      <Dialog.Content className="max-h-[85vh] max-w-2xl overflow-y-auto">
        <Dialog.Header>
          <Dialog.Title>Add guardrail</Dialog.Title>
          <Dialog.Description>
            Inspect traffic through {serverName}. The guardrail is scoped to
            this server and its tools only.
          </Dialog.Description>
        </Dialog.Header>
        <div className="space-y-6">
          <div className="space-y-1">
            <label
              htmlFor="server-guardrail-name"
              className="text-sm leading-none font-medium"
            >
              Name (optional)
            </label>
            <Input
              id="server-guardrail-name"
              placeholder={defaultServerGuardrailName(serverName)}
              value={name}
              onChange={setName}
            />
          </div>
          <ServerGuardrailsForm
            state={state}
            onChange={(update) => setState(update)}
            toolsSource={toolsSource}
            serverName={serverName}
            mode={mode}
          />
          {error ? (
            <Alert variant="error" dismissible={false}>
              {error}
            </Alert>
          ) : null}
        </div>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => close(false)}>
            Cancel
          </Button>
          <Button onClick={() => void submit()} disabled={guardrail.isPending}>
            <Button.Text>
              {guardrail.isPending ? "Creating…" : "Create guardrail"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
