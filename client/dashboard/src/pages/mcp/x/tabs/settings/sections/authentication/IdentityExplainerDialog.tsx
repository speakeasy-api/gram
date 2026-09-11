import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { ArrowDown, Bot, Check, Server, User } from "lucide-react";
import type { ReactNode } from "react";

type FlowNode = { icon: ReactNode; label: string };

/**
 * One hop of the credential chain: a labelled node, then the thing handed to
 * the next node. Reading the two columns side by side is the whole point of
 * the dialog — the only difference between the modes is what reaches the
 * upstream service.
 */
function Flow({
  nodes,
  edges,
}: {
  nodes: [FlowNode, FlowNode, FlowNode];
  edges: [string, string];
}): JSX.Element {
  return (
    <div className="flex flex-col items-start gap-1.5">
      {nodes.map((node, index) => (
        <div key={node.label} className="flex flex-col items-start gap-1.5">
          <div className="flex items-center gap-2">
            <span className="bg-card flex size-7 shrink-0 items-center justify-center border">
              {node.icon}
            </span>
            <span className="font-mono text-sm">{node.label}</span>
          </div>
          {index < edges.length ? (
            <div className="text-muted-foreground ml-3 flex items-center gap-2 border-l pl-4">
              <span className="font-mono text-xs">{edges[index]}</span>
              <ArrowDown aria-hidden="true" className="size-3" />
            </div>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function UseItWhen({ items }: { items: string[] }): JSX.Element {
  return (
    <>
      <Text small className="mt-3 block border-t pt-3 font-medium">
        Use it when
      </Text>
      <ul className="mt-2 space-y-1.5">
        {items.map((item) => (
          <li key={item} className="flex items-start gap-2">
            <Check
              aria-hidden="true"
              className="text-muted-foreground mt-0.5 size-3.5 shrink-0"
            />
            <Text muted small>
              {item}
            </Text>
          </li>
        ))}
      </ul>
    </>
  );
}

/**
 * The User Identity versus Agent Identity explainer. Opened from the identity
 * pill on a server's details page and from the identity choice in the add-server
 * flow, so both surfaces answer the question the same way.
 */
export function IdentityExplainerDialog({
  open,
  onOpenChange,
  trigger,
}: {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  trigger?: ReactNode;
}): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger ? <Dialog.Trigger asChild>{trigger}</Dialog.Trigger> : null}
      <Dialog.Content className="max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>User Identity or Agent Identity?</Dialog.Title>
          <Dialog.Description>
            Users of this server must authenticate as themselves against the
            service&apos;s identity provider, or every caller can act as one
            Agent Identity, a service account whose permissions you manage in
            the control plane.
          </Dialog.Description>
        </Dialog.Header>
        <div className="grid gap-6 sm:grid-cols-2">
          <div>
            <Text variant="small" mono className="text-eyebrow block uppercase">
              User Identity
            </Text>
            <div className="mt-3">
              <Flow
                nodes={[
                  {
                    icon: <User aria-hidden="true" className="size-3.5" />,
                    label: "Each user",
                  },
                  {
                    icon: <Server aria-hidden="true" className="size-3.5" />,
                    label: "MCP server",
                  },
                  {
                    icon: <Bot aria-hidden="true" className="size-3.5" />,
                    label: "Service",
                  },
                ]}
                edges={["signs in", "their own token"]}
              />
            </div>
            <Text
              muted
              variant="small"
              className="mt-3 block font-mono text-xs"
            >
              Permissions: the user&apos;s own, set in the service
            </Text>
            <UseItWhen
              items={[
                "Users should carry their full scope of permission into the agent.",
                "Permissions stay fully managed in the backing service.",
                "The service has no headless auth mechanism.",
              ]}
            />
          </div>
          <div>
            <Text variant="small" mono className="text-eyebrow block uppercase">
              Agent Identity
            </Text>
            <div className="mt-3">
              <Flow
                nodes={[
                  {
                    icon: <User aria-hidden="true" className="size-3.5" />,
                    label: "Every user",
                  },
                  {
                    icon: <Server aria-hidden="true" className="size-3.5" />,
                    label: "MCP server",
                  },
                  {
                    icon: <Bot aria-hidden="true" className="size-3.5" />,
                    label: "Service",
                  },
                ]}
                edges={["signs in", "one service account"]}
              />
            </div>
            <Text
              muted
              variant="small"
              className="mt-3 block font-mono text-xs"
            >
              Permissions: the agent&apos;s, set in the control plane
            </Text>
            <UseItWhen
              items={[
                "Limit what an agent with bad judgment can do wherever this server is deployed.",
                "Actions should be attributed to the agent in the upstream service.",
                "The service does not support OAuth login.",
              ]}
            />
          </div>
        </div>
        <Dialog.Footer>
          <Dialog.Close asChild>
            <Button variant="secondary">
              <Button.Text>Close</Button.Text>
            </Button>
          </Dialog.Close>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
