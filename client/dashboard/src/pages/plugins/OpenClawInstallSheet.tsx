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
                  "If help does not list --accept-capabilities, omit it. Omit --force too if it is not listed. These flags confirm the local source and its declared capabilities; --force also replaces an existing installation.",
                code: `openclaw plugins install --force --accept-capabilities ${shellQuote(`./${archiveName}`)}`,
              },
              {
                title: "Enable conversation access",
                description:
                  "This allows the plugin to capture prompts, responses, and usage alongside tool calls.",
                code: "openclaw config set plugins.entries.speakeasy-observability.hooks.allowConversationAccess true",
              },
              {
                title: "Verify the installation",
                description:
                  "Follow the install command’s restart instructions. Restart the Gateway after the current turn if requested, so the agent can finish reporting the install result. Then run a tool-using turn and check Agent Sessions in Speakeasy. Models routed through the Claude CLI need Claude Code hooks installed separately.",
                code: "openclaw plugins inspect speakeasy-observability\nopenclaw plugins doctor",
              },
            ]}
          />
        </div>
      </SheetContent>
    </Sheet>
  );
}
