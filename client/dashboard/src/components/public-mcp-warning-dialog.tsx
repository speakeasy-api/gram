import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/Accordion";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useListToolSchemaStaticValues } from "@gram/client/react-query/listToolSchemaStaticValues.js";
import { ExternalLink, ShieldAlert } from "lucide-react";

interface PublicMcpWarningDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  toolsetSlug: string;
  environmentSlug: string;
  variableNames: string[];
}

export function PublicMcpWarningDialog({
  isOpen,
  onClose,
  onConfirm,
  isLoading = false,
  toolsetSlug,
  environmentSlug,
  variableNames,
}: PublicMcpWarningDialogProps): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const staticValues = useListToolSchemaStaticValues(
    { slug: toolsetSlug, gramProject },
    undefined,
    {
      enabled: isOpen,
      retry: false,
      throwOnError: false,
    },
  );

  const handleConfirm = () => {
    onConfirm();
    onClose();
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="border-destructive max-w-2xl overflow-hidden border-t-2 p-0">
        <div className="p-6">
          <Dialog.Header>
            <Dialog.Title className="flex items-center gap-2">
              <ShieldAlert
                className="text-destructive h-5 w-5 shrink-0"
                aria-hidden="true"
              />
              Review public server values
            </Dialog.Title>
            <Dialog.Description>
              Anyone with this server&apos;s URL can read its tool definitions.
              We recommend you review the static values in each tool&apos;s
              input schema and confirm they do not contain sensitive
              information. These values will be visible to anyone who can view
              this server&apos;s tools.
            </Dialog.Description>
          </Dialog.Header>

          <div className="mt-4 max-h-[60vh] space-y-5 overflow-y-auto pr-2 text-sm">
            {variableNames.length > 0 && (
              <div className="space-y-2">
                <p className="text-eyebrow">Used by every public caller</p>
                <Text className="text-muted-foreground">
                  Anyone with this URL will call with values from the Default
                  Environment. System values are shared. Treat them as team or
                  public credentials, not user credentials.
                </Text>
                <ul className="border-border bg-muted/30 max-h-40 space-y-1 overflow-y-auto border p-3 font-mono">
                  {variableNames.map((name) => (
                    <li key={name} className="text-sm font-light">
                      {name}
                    </li>
                  ))}
                </ul>
                <a
                  href={`/environments/${environmentSlug}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-foreground inline-flex items-center gap-1 text-sm underline-offset-4 hover:underline"
                >
                  Review in &quot;Default Environment&quot;
                  <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
                </a>
              </div>
            )}

            <div className="space-y-2">
              <p className="text-eyebrow">Tool schema static values</p>
              {staticValues.isPending && (
                <Text className="text-muted-foreground">
                  Loading tool schema values...
                </Text>
              )}
              {staticValues.isFetching && !staticValues.isPending && (
                <Text className="text-muted-foreground">
                  Refreshing tool schema values...
                </Text>
              )}
              {staticValues.isError && (
                <div className="border-destructive space-y-3 border p-3">
                  <Text>Tool schema values could not be loaded.</Text>
                  <Button
                    variant="tertiary"
                    size="sm"
                    onClick={() => void staticValues.refetch()}
                  >
                    Try again
                  </Button>
                </div>
              )}
              {staticValues.isSuccess &&
                staticValues.data.tools.length === 0 && (
                  <Text className="text-muted-foreground">
                    No static values were found in this server&apos;s tool
                    schemas.
                  </Text>
                )}
              {staticValues.isSuccess && staticValues.data.tools.length > 0 && (
                <Accordion
                  type="multiple"
                  className="border-border border px-3"
                >
                  {staticValues.data.tools.map((tool) => (
                    <AccordionItem key={tool.toolUrn} value={tool.toolUrn}>
                      <AccordionTrigger>
                        <span className="flex min-w-0 items-center gap-2">
                          <span className="truncate font-mono">
                            {tool.toolName}
                          </span>
                          <span className="text-muted-foreground text-xs">
                            {tool.values.length}
                          </span>
                        </span>
                      </AccordionTrigger>
                      <AccordionContent className="space-y-3">
                        {tool.values.map((value, index) => (
                          <div
                            key={`${value.schemaPath}:${value.keyword}:${index}`}
                            className="border-border space-y-2 border p-3"
                          >
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-eyebrow">
                                {value.keyword}
                              </span>
                              <code className="text-muted-foreground break-all text-xs">
                                {value.schemaPath || "(root)"}
                              </code>
                            </div>
                            <pre className="bg-muted/30 overflow-x-auto p-2 font-mono text-xs whitespace-pre-wrap break-all">
                              {value.valueJson}
                            </pre>
                          </div>
                        ))}
                      </AccordionContent>
                    </AccordionItem>
                  ))}
                </Accordion>
              )}
            </div>
          </div>

          <Dialog.Footer className="mt-6 gap-2">
            <Button variant="tertiary" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              onClick={handleConfirm}
              disabled={
                isLoading || !staticValues.isSuccess || staticValues.isFetching
              }
            >
              {isLoading ? "Publishing..." : "Make public"}
            </Button>
          </Dialog.Footer>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
