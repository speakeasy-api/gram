import { CodeBlock } from "@/components/code";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/dialog";
import { Label as Heading } from "@/components/ui/label";
import { Link } from "@/components/ui/link";
import { Type } from "@/components/ui/type";
import { useMcpUrl } from "@/hooks/useToolsetUrl";
import { Toolset } from "@/lib/toolTypes";
import { Button, cn, Icon, Input, Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { CompactUpload } from "../upload";
import type { UseMcpMetadataMetadataFormResult } from "./useMcpMetadataForm";

interface ConfigFormProps {
  toolset: Toolset;
  form: UseMcpMetadataMetadataFormResult;
  isLoading: boolean;
}

export function InstallPageConfigForm({
  toolset,
  form,
  isLoading,
}: ConfigFormProps) {
  const { installPageUrl } = useMcpUrl(toolset);
  const [open, setOpen] = useState(false);

  if (!installPageUrl) {
    return null;
  }

  return (
    <div className="bg-muted/20 flex items-center gap-2 rounded-lg border p-2">
      <CodeBlock
        className="flex-grow overflow-hidden"
        innerClassName="!p-2 !pr-10 !bg-white dark:!bg-zinc-950"
        preClassName="whitespace-nowrap overflow-auto"
        copyable={true}
      >
        {installPageUrl}
      </CodeBlock>
      <Dialog
        open={open}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            form.resetBranding();
          }
          setOpen(nextOpen);
        }}
      >
        <RequireScope scope="mcp:write" level="component">
          <Dialog.Trigger asChild>
            <Button variant="secondary">
              <Button.LeftIcon>
                <Icon name="palette" />
              </Button.LeftIcon>
              <Button.Text>Edit Branding</Button.Text>
            </Button>
          </Dialog.Trigger>
        </RequireScope>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Install Page Configuration</Dialog.Title>
          </Dialog.Header>
          <Stack className={cn("gap-4", isLoading && "animate-pulse")}>
            <div>
              <Heading> MCP Logo </Heading>
              <Type muted small className="max-w-2xl">
                The logo presented on this page
              </Type>
            </div>
            <div className="inline-block">
              <CompactUpload
                allowedExtensions={["png", "jpg", "jpeg"]}
                onUpload={form.logoUploadHandlers.onUpload}
                renderFilePreview={form.logoUploadHandlers.renderFilePreview}
                className="max-h-[200px] w-full"
              />
            </div>
            <div>
              <Heading> Documentation Link </Heading>
              <Type muted small className="max-w-2xl">
                A link to your own MCP documentation that will be featured at
                the top of the page
              </Type>
            </div>
            <div className="relative">
              <Input
                type="text"
                placeholder="https://my-documentation.link"
                className="w-full"
                {...form.urlInputHandlers}
              />
              {form.valid.message && (
                <span className="text-destructive absolute -bottom-4 left-0 text-xs">
                  {form.valid.message}
                </span>
              )}
            </div>
            <div>
              <Heading> Documentation Text </Heading>
              <Type muted small className="max-w-2xl">
                What your custom link will say on the MCP page
              </Type>
            </div>
            <div className="relative">
              <Input
                type="text"
                placeholder="View Docs"
                className="w-full"
                {...form.docsTextInputHandlers}
              />
              {form.valid.message && (
                <span className="text-destructive absolute -bottom-4 left-0 text-xs">
                  {form.valid.message}
                </span>
              )}
            </div>
            <div>
              <Heading> Installation Override URL </Heading>
              <Type muted small className="max-w-2xl">
                A URL to redirect to instead of the default installation page
                when someone navigates to your MCP URL in their browser.
              </Type>
            </div>
            <div className="relative">
              <Input
                type="text"
                placeholder="Leave unset to use the default installation page"
                className="w-full"
                {...form.installationOverrideUrlInputHandlers}
              />
              {form.valid.message && (
                <span className="text-destructive absolute -bottom-4 left-0 text-xs">
                  {form.valid.message}
                </span>
              )}
            </div>
          </Stack>
          <Dialog.Footer>
            <Button
              variant="tertiary"
              disabled={!form.brandingDirty}
              onClick={form.resetBranding}
            >
              <Button.Text>Discard</Button.Text>
            </Button>
            <Button
              onClick={() => {
                form.save();
                setOpen(false);
              }}
              disabled={isLoading || !form.valid.valid || !form.brandingDirty}
            >
              <Button.Text>Save</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
      <Link external to={installPageUrl} noIcon>
        <Button variant="primary" className="px-4">
          <Button.LeftIcon>
            <Icon name="external-link" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>View</Button.Text>
        </Button>
      </Link>
    </div>
  );
}
