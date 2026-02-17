import { CodeBlock } from "@/components/code";
import { Dialog } from "@speakeasy-api/moonshine";

interface UpdateFunctionDialogContentProps {
  functionSlug: string;
  onClose: () => void;
}

export function UpdateFunctionDialogContent({
  functionSlug,
  onClose,
}: UpdateFunctionDialogContentProps) {
  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Update Function</Dialog.Title>
        <Dialog.Description>
          Deploy a new version of function {functionSlug} using the Gram CLI
        </Dialog.Description>
      </Dialog.Header>
      <div className="space-y-3">
        <div>
          <p className="text-xs text-muted-foreground mb-1.5">
            Direct upload
          </p>
          <CodeBlock
            language="bash"
            className="!border-0 !bg-muted/50 !rounded-lg"
          >
            {`gram upload --type function \\\n  --slug ${functionSlug} \\\n  --location ./dist/functions.zip`}
          </CodeBlock>
        </div>
        <div>
          <p className="text-xs text-muted-foreground mb-1.5">
            Or stage and push (useful for CI/CD)
          </p>
          <CodeBlock
            language="bash"
            className="!border-0 !bg-muted/50 !rounded-lg"
          >
            {`gram stage function \\\n  --slug ${functionSlug} \\\n  --location ./dist/functions.zip\n\ngram push`}
          </CodeBlock>
        </div>
      </div>
      <Dialog.Footer>
        <Dialog.Close onClick={onClose}>Close</Dialog.Close>
      </Dialog.Footer>
    </>
  );
}
