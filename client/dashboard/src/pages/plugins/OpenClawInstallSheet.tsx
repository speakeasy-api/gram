import { CodeBlock } from "@/components/code";
import { InstallSteps } from "@/components/install-steps";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { shellQuote } from "@/lib/shell";

export function OpenClawInstallSheet({
  archiveName,
  onClose,
}: {
  archiveName: string;
  onClose: () => void;
}): JSX.Element {
  const archivePath = shellQuote(`./${archiveName}`);

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Install OpenClaw observability</SheetTitle>
          <SheetDescription>
            Your ZIP is downloaded. Run these commands in the folder where you
            saved it. If your browser renamed the file, update the filename.
          </SheetDescription>
        </SheetHeader>
        <div className="px-4 pb-4">
          <InstallSteps
            steps={[
              {
                title: "Check the supported install flags",
                description:
                  "OpenClaw 2026.8.1 and later require source confirmation and capability consent for a local ZIP. Older releases differ; use the flags listed by your installed command.",
                code: "openclaw plugins install --help",
              },
              {
                title: "Install the downloaded plugin",
                description:
                  "Run the command matching your help output. These flags confirm the local source and its declared capabilities; --force also replaces an existing installation.",
                children: (
                  <div className="space-y-3">
                    {[
                      {
                        label: "Both flags listed (2026.8.1 and later)",
                        flags: "--force --accept-capabilities ",
                      },
                      {
                        label: "Only --force listed (older backports)",
                        flags: "--force ",
                      },
                      { label: "Neither flag listed", flags: "" },
                    ].map(({ label, flags }) => (
                      <div key={label} className="space-y-1">
                        <p className="text-muted-foreground text-sm">{label}</p>
                        <CodeBlock language="bash" className="bg-background">
                          {`openclaw plugins install ${flags}${archivePath}`}
                        </CodeBlock>
                      </div>
                    ))}
                  </div>
                ),
              },
              {
                title: "Enable conversation access",
                description:
                  "This allows the plugin to capture prompts, responses, and usage alongside tool calls.",
                code: "openclaw config set plugins.entries.speakeasy-observability.hooks.allowConversationAccess true",
              },
              {
                title: "Restart and verify the installation",
                description:
                  "Restart the Gateway after installing the plugin. If an agent is installing it, wait until its current turn finishes. Then run a tool-using turn and check Agent Sessions in Speakeasy. Models routed through the Claude CLI need Claude Code hooks installed separately.",
                code: "openclaw gateway restart\nopenclaw plugins inspect speakeasy-observability\nopenclaw plugins doctor",
              },
            ]}
          />
        </div>
      </SheetContent>
    </Sheet>
  );
}
