import { CodeBlock } from "@/components/code";
import { Dialog } from "@/components/ui/Dialog";
import { Link } from "@/components/ui/Link";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { AlertTriangle } from "lucide-react";

export function AutoRegisterFailedStep({
  error,
  onClose,
}: {
  error: string | null;
  onClose: () => void;
}): JSX.Element {
  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 py-8">
        <AlertTriangle className="text-destructive h-12 w-12" />
        <Text className="text-center text-lg font-medium">
          OAuth Setup Failed
        </Text>
        <Text muted small className="max-w-md text-center">
          {error ? (
            <CodeBlock innerClassName="break-normal">{error}</CodeBlock>
          ) : (
            "We weren't able to set up OAuth for this MCP server."
          )}
        </Text>
        <Text muted small className="max-w-md text-center">
          Please reach out to
          <Link
            className="inline-block px-1"
            href="mailto:support@speakeasy.com"
          >
            customer support
          </Link>
          , and we'll help you get this MCP server connected.
        </Text>
      </div>

      <Dialog.Footer className="flex justify-end">
        <Button onClick={onClose}>Close</Button>
      </Dialog.Footer>
    </>
  );
}
