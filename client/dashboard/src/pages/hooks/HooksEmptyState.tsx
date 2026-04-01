import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { ExternalLink, Workflow } from "lucide-react";
import { useState } from "react";

// Claude Code logo - official Anthropic Claude icon
function ClaudeCodeIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 512 509.64"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        fill="#D77655"
        d="M115.612 0h280.775C459.974 0 512 52.026 512 115.612v278.415c0 63.587-52.026 115.612-115.613 115.612H115.612C52.026 509.639 0 457.614 0 394.027V115.612C0 52.026 52.026 0 115.612 0z"
      />
      <path
        fill="#FCF2EE"
        fillRule="nonzero"
        d="M142.27 316.619l73.655-41.326 1.238-3.589-1.238-1.996-3.589-.001-12.31-.759-42.084-1.138-36.498-1.516-35.361-1.896-8.897-1.895-8.34-10.995.859-5.484 7.482-5.03 10.717.935 23.683 1.617 35.537 2.452 25.782 1.517 38.193 3.968h6.064l.86-2.451-2.073-1.517-1.618-1.517-36.776-24.922-39.81-26.338-20.852-15.166-11.273-7.683-5.687-7.204-2.451-15.721 10.237-11.273 13.75.935 3.513.936 13.928 10.716 29.749 23.027 38.848 28.612 5.687 4.727 2.275-1.617.278-1.138-2.553-4.271-21.13-38.193-22.546-38.848-10.035-16.101-2.654-9.655c-.935-3.968-1.617-7.304-1.617-11.374l11.652-15.823 6.445-2.073 15.545 2.073 6.547 5.687 9.655 22.092 15.646 34.78 24.265 47.291 7.103 14.028 3.791 12.992 1.416 3.968 2.449-.001v-2.275l1.997-26.641 3.69-32.707 3.589-42.084 1.239-11.854 5.863-14.206 11.652-7.683 9.099 4.348 7.482 10.716-1.036 6.926-4.449 28.915-8.72 45.294-5.687 30.331h3.313l3.792-3.791 15.342-20.372 25.782-32.227 11.374-12.789 13.27-14.129 8.517-6.724 16.1-.001 11.854 17.617-5.307 18.199-16.581 21.029-13.75 17.819-19.716 26.54-12.309 21.231 1.138 1.694 2.932-.278 44.536-9.479 24.062-4.347 28.714-4.928 12.992 6.066 1.416 6.167-5.106 12.613-30.71 7.583-36.018 7.204-53.636 12.689-.657.48.758.935 24.164 2.275 10.337.556h25.301l47.114 3.514 12.309 8.139 7.381 9.959-1.238 7.583-18.957 9.655-25.579-6.066-59.702-14.205-20.474-5.106-2.83-.001v1.694l17.061 16.682 31.266 28.233 39.152 36.397 1.997 8.999-5.03 7.102-5.307-.758-34.401-25.883-13.27-11.651-30.053-25.302-1.996-.001v2.654l6.926 10.136 36.574 54.975 1.895 16.859-2.653 5.485-9.479 3.311-10.414-1.895-21.408-30.054-22.092-33.844-17.819-30.331-2.173 1.238-10.515 113.261-4.929 5.788-11.374 4.348-9.478-7.204-5.03-11.652 5.03-23.027 6.066-30.052 4.928-23.886 4.449-29.674 2.654-9.858-.177-.657-2.173.278-22.37 30.71-34.021 45.977-26.919 28.815-6.445 2.553-11.173-5.789 1.037-10.337 6.243-9.2 37.257-47.392 22.47-29.371 14.508-16.961-.101-2.451h-.859l-98.954 64.251-17.618 2.275-7.583-7.103.936-11.652 3.589-3.791 29.749-20.474-.101.102.024.101z"
      />
    </svg>
  );
}

// Cursor logo - official cursor cube icon
function CursorIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 466.73 532.09"
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        fill="currentColor"
        d="M457.43,125.94L244.42,2.96c-6.84-3.95-15.28-3.95-22.12,0L9.3,125.94c-5.75,3.32-9.3,9.46-9.3,16.11v247.99c0,6.65,3.55,12.79,9.3,16.11l213.01,122.98c6.84,3.95,15.28,3.95,22.12,0l213.01-122.98c5.75-3.32,9.3-9.46,9.3-16.11v-247.99c0-6.65-3.55-12.79-9.3-16.11h-.01ZM444.05,151.99l-205.63,356.16c-1.39,2.4-5.06,1.42-5.06-1.36v-233.21c0-4.66-2.49-8.97-6.53-11.31L24.87,145.67c-2.4-1.39-1.42-5.06,1.36-5.06h411.26c5.84,0,9.49,6.33,6.57,11.39h-.01Z"
      />
    </svg>
  );
}

// Codex logo - official OpenAI monoblossom icon
function CodexIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 721 721"
      xmlns="http://www.w3.org/2000/svg"
    >
      <g clipPath="url(#clip0_codex)">
        <g clipPath="url(#clip1_codex)">
          <path
            d="M304.246 294.611V249.028C304.246 245.189 305.687 242.309 309.044 240.392L400.692 187.612C413.167 180.415 428.042 177.058 443.394 177.058C500.971 177.058 537.44 221.682 537.44 269.182C537.44 272.54 537.44 276.379 536.959 280.218L441.954 224.558C436.197 221.201 430.437 221.201 424.68 224.558L304.246 294.611ZM518.245 472.145V363.224C518.245 356.505 515.364 351.707 509.608 348.349L389.174 278.296L428.519 255.743C431.877 253.826 434.757 253.826 438.115 255.743L529.762 308.523C556.154 323.879 573.905 356.505 573.905 388.171C573.905 424.636 552.315 458.225 518.245 472.141V472.145ZM275.937 376.182L236.592 353.152C233.235 351.235 231.794 348.354 231.794 344.515V238.956C231.794 187.617 271.139 148.749 324.4 148.749C344.555 148.749 363.264 155.468 379.102 167.463L284.578 222.164C278.822 225.521 275.942 230.319 275.942 237.039V376.186L275.937 376.182ZM360.626 425.122L304.246 393.455V326.283L360.626 294.616L417.002 326.283V393.455L360.626 425.122ZM396.852 570.989C376.698 570.989 357.989 564.27 342.151 552.276L436.674 497.574C442.431 494.217 445.311 489.419 445.311 482.699V343.552L485.138 366.582C488.495 368.499 489.936 371.379 489.936 375.219V480.778C489.936 532.117 450.109 570.985 396.852 570.985V570.989ZM283.134 463.99L191.486 411.211C165.094 395.854 147.343 363.229 147.343 331.562C147.343 294.616 169.415 261.509 203.48 247.593V356.991C203.48 363.71 206.361 368.508 212.117 371.866L332.074 441.437L292.729 463.99C289.372 465.907 286.491 465.907 283.134 463.99ZM277.859 542.68C223.639 542.68 183.813 501.895 183.813 451.514C183.813 447.675 184.294 443.836 184.771 439.997L279.295 494.698C285.051 498.056 290.812 498.056 296.568 494.698L417.002 425.127V470.71C417.002 474.549 415.562 477.429 412.204 479.346L320.557 532.126C308.081 539.323 293.206 542.68 277.854 542.68H277.859ZM396.852 599.776C454.911 599.776 503.37 558.513 514.41 503.812C568.149 489.896 602.696 439.515 602.696 388.176C602.696 354.587 588.303 321.962 562.392 298.45C564.791 288.373 566.231 278.296 566.231 268.224C566.231 199.611 510.571 148.267 446.274 148.267C433.322 148.267 420.846 150.184 408.37 154.505C386.775 133.392 357.026 119.958 324.4 119.958C266.342 119.958 217.883 161.22 206.843 215.921C153.104 229.837 118.557 280.218 118.557 331.557C118.557 365.146 132.95 397.771 158.861 421.283C156.462 431.36 155.022 441.437 155.022 451.51C155.022 520.123 210.682 571.466 274.978 571.466C287.931 571.466 300.407 569.549 312.883 565.228C334.473 586.341 364.222 599.776 396.852 599.776Z"
            fill="currentColor"
          />
        </g>
      </g>
      <defs>
        <clipPath id="clip0_codex">
          <rect
            width="720"
            height="720"
            fill="white"
            transform="translate(0.606934 0.0999756)"
          />
        </clipPath>
        <clipPath id="clip1_codex">
          <rect
            width="484.139"
            height="479.818"
            fill="white"
            transform="translate(118.557 119.958)"
          />
        </clipPath>
      </defs>
    </svg>
  );
}

interface ProviderCardProps {
  name: string;
  icon: React.ComponentType<{ className?: string }>;
  status: "available" | "coming-soon";
  onInstall: () => void;
}

function ProviderCard({
  name,
  icon: IconComponent,
  status,
  onInstall,
}: ProviderCardProps) {
  const isComingSoon = status === "coming-soon";

  return (
    <button
      onClick={onInstall}
      className={cn(
        "relative flex flex-col items-center p-6 rounded-lg border transition-all min-w-[160px]",
        status === "available"
          ? "border-border hover:border-primary hover:bg-muted/50 cursor-pointer"
          : "border-border/50 hover:border-primary/50 hover:bg-muted/30 cursor-pointer opacity-60",
      )}
    >
      <IconComponent className="size-12 mb-3" />
      <span className="font-medium text-sm">{name}</span>
      {isComingSoon && (
        <div className="absolute top-3 right-3">
          <span className="text-[10px] font-semibold text-muted-foreground bg-muted px-2 py-0.5 rounded-full uppercase tracking-wide">
            Coming Soon
          </span>
        </div>
      )}
    </button>
  );
}

interface ClaudeInstallModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function ClaudeInstallModal({ open, onOpenChange }: ClaudeInstallModalProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-4xl">
        <Dialog.Header>
          <Dialog.Title>Install Gram Plugins for Claude Code</Dialog.Title>
        </Dialog.Header>

        <div className="space-y-6">
          {/* Test Yourself Section */}
          <div>
            <h3 className="text-sm font-semibold mb-2">Test Yourself</h3>
            <p className="text-sm text-muted-foreground mb-4">
              Add the Gram marketplace and install the plugins you need:
            </p>
            <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm space-y-2">
              <div className="flex items-center justify-between">
                <code>claude plugin marketplace add speakeasy-api/gram</code>
              </div>
              <div className="flex items-center justify-between">
                <code>claude plugin install gram-hooks@gram</code>
                <span className="text-muted-foreground text-xs ml-2">
                  # observability
                </span>
              </div>
              <div className="flex items-center justify-between">
                <code>claude plugin install gram-skills@gram</code>
                <span className="text-muted-foreground text-xs ml-2">
                  # deployment workflows
                </span>
              </div>
            </div>
          </div>

          {/* Distribute to Team Section */}
          <div>
            <h3 className="text-sm font-semibold mb-2">
              Distribute to Your Team
            </h3>
            <p className="text-sm text-muted-foreground mb-4">
              Require your team to use Gram plugins by configuring their Claude
              Code settings:
            </p>

            <div className="space-y-4">
              <div>
                <h4 className="text-xs font-medium text-muted-foreground mb-2">
                  1. Require the marketplace
                </h4>
                <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
                  <code>
                    {`{
  "pluginMarketplaces": {
    "required": ["speakeasy-api/gram"]
  }
}`}
                  </code>
                </div>
              </div>

              <div>
                <h4 className="text-xs font-medium text-muted-foreground mb-2">
                  2. Require the plugin
                </h4>
                <div className="bg-muted/50 rounded-lg p-4 font-mono text-sm">
                  <code>
                    {`{
  "plugins": {
    "required": ["gram-hooks@gram", "gram-skills@gram"]
  }
}`}
                  </code>
                </div>
              </div>

              <Button variant="outline" size="sm" asChild>
                <a
                  href="https://code.claude.com/docs/en/plugin-marketplaces#require-marketplaces-for-your-team"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-2"
                >
                  <ExternalLink className="size-4" />
                  View Full Documentation
                </a>
              </Button>
            </div>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

export function HooksEmptyState() {
  const [showClaudeModal, setShowClaudeModal] = useState(false);
  const [showFeatureRequestModal, setShowFeatureRequestModal] = useState(false);
  const [selectedProvider, setSelectedProvider] = useState<string>("");

  const handleProviderClick = (provider: string, status: string) => {
    if (status === "coming-soon") {
      setSelectedProvider(provider);
      setShowFeatureRequestModal(true);
      return;
    }

    if (provider === "claude") {
      setShowClaudeModal(true);
    }
  };

  return (
    <>
      <div className="flex flex-col items-center justify-center py-16 px-4">
        <div className="max-w-2xl w-full text-center space-y-8">
          {/* Icon and Title */}
          <div className="flex flex-col items-center gap-4">
            <div className="size-16 rounded-full bg-muted flex items-center justify-center">
              <Icon name="workflow" className="size-8 text-muted-foreground" />
            </div>
            <div>
              <h2 className="text-xl font-semibold mb-2">No Hook Logs Yet</h2>
              <p className="text-sm text-muted-foreground max-w-md mx-auto">
                Install Gram Hooks in your AI coding assistant to start
                capturing tool execution logs
              </p>
            </div>
          </div>

          {/* Installation Options */}
          <div>
            <h3 className="text-sm font-medium mb-4">
              Choose Your AI Coding Assistant
            </h3>
            <div className="flex items-center justify-center gap-4">
              <ProviderCard
                name="Claude Code"
                icon={ClaudeCodeIcon}
                status="available"
                onInstall={() => handleProviderClick("claude", "available")}
              />
              <ProviderCard
                name="Cursor"
                icon={CursorIcon}
                status="coming-soon"
                onInstall={() => handleProviderClick("cursor", "coming-soon")}
              />
              <ProviderCard
                name="Codex"
                icon={CodexIcon}
                status="coming-soon"
                onInstall={() => handleProviderClick("codex", "coming-soon")}
              />
            </div>
          </div>
        </div>
      </div>

      {/* Claude Install Modal */}
      <ClaudeInstallModal
        open={showClaudeModal}
        onOpenChange={setShowClaudeModal}
      />

      {/* Feature Request Modal */}
      <FeatureRequestModal
        isOpen={showFeatureRequestModal}
        onClose={() => setShowFeatureRequestModal(false)}
        title={`${selectedProvider.charAt(0).toUpperCase() + selectedProvider.slice(1)} Integration`}
        description={`Support for ${selectedProvider.charAt(0).toUpperCase() + selectedProvider.slice(1)} is coming soon. Let us know you're interested and we'll notify you when it's available.`}
        actionType={`hooks_${selectedProvider}_integration`}
        icon={Workflow}
        telemetryData={{ provider: selectedProvider }}
      />
    </>
  );
}
