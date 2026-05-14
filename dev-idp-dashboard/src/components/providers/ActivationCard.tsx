import { useState } from "react";
import { motion, AnimatePresence } from "motion/react";
import { Check, CircleCheck, Copy } from "lucide-react";
import { match } from "ts-pattern";
import { cn } from "@/lib/utils";
import { Card, CardContent } from "@/components/ui/card";
import { useGramMode } from "@/hooks/use-gram-mode";
import type { ActivatableMode } from "@/lib/provider-info";

const WORKOS_API_KEYS_URL =
  "https://dashboard.workos.com/environment_01J5C09A9KMAHSZ0T9WBK3TXHJ/api-keys";

export function ActivationCard({ mode }: { mode: ActivatableMode }) {
  const { data, isLoading } = useGramMode();
  const isActive = data?.mode === mode;

  return (
    <Card size="sm" className={cn("!rounded-md w-80 shrink-0")}>
      <CardContent>
        {isLoading ? (
          <div className="text-xs text-muted-foreground">Loading…</div>
        ) : isActive ? (
          <ActiveState />
        ) : (
          <InactiveState mode={mode} />
        )}
      </CardContent>
    </Card>
  );
}

function ActiveState() {
  return (
    <div className="flex items-start gap-2.5">
      <CircleCheck
        className="text-[var(--retro-green)] size-5 shrink-0 mt-0.5"
        strokeWidth={2.5}
      />
      <div className="min-w-0">
        <div className="font-semibold text-sm">Active</div>
        <div className="text-xs text-muted-foreground">
          Gram is currently using this provider.
        </div>
      </div>
    </div>
  );
}

function InactiveState({ mode }: { mode: ActivatableMode }) {
  const { data } = useGramMode();
  const workosKeySet =
    data?.meta.env.find((v) => v.name === "WORKOS_API_KEY")?.is_set ?? false;

  return (
    <div className="space-y-3">
      <Header />
      {match(mode)
        .with("local-speakeasy", () => (
          <CopyableCommand command="mise set --file mise.local.toml SPEAKEASY_SERVER_ADDRESS={{env.GRAM_DEVIDP_EXTERNAL_URL}}/local-speakeasy" />
        ))
        .with("workos", () => (
          <>
            <CopyableCommand
              command={`mise set --file mise.local.toml \\
  SPEAKEASY_SERVER_ADDRESS={{env.GRAM_DEVIDP_EXTERNAL_URL}}/workos \\
  WORKOS_API_URL=https://api.workos.com`}
            />
            {!workosKeySet && (
              <div className="space-y-2 pt-3 border-t border-border">
                <CopyableCommand command="mise set --file mise.local.toml WORKOS_API_KEY=<paste-key>" />
                <p className="text-xs text-muted-foreground">
                  Grab a fresh API key from{" "}
                  <a
                    href={WORKOS_API_KEYS_URL}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline underline-offset-2 hover:text-[var(--retro-orange)]"
                  >
                    the WorkOS dashboard
                  </a>{" "}
                  and substitute it into the command.
                </p>
              </div>
            )}
          </>
        ))
        .exhaustive()}
    </div>
  );
}

function Header() {
  return (
    <div>
      <div className="font-semibold text-sm">Activate</div>
      <div className="text-xs text-muted-foreground">
        Set the env vars below, then restart Gram.
      </div>
    </div>
  );
}

/**
 * `<pre>` with an absolutely-positioned copy button hovering over the
 * top-right corner. Click → write to clipboard, swap glyph to a check for
 * 1.2s, fade back.
 */
function CopyableCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1200);
    } catch {
      // clipboard API may be unavailable in some embedded contexts; silently ignore
    }
  };

  return (
    <div className="relative group">
      <pre className="text-[11px] font-mono leading-relaxed bg-muted rounded-sm p-2 pr-9 overflow-x-auto whitespace-pre">
        <code>{command}</code>
      </pre>
      <button
        type="button"
        onClick={onCopy}
        aria-label={copied ? "Copied" : "Copy command"}
        className={cn(
          "absolute top-1 right-1 inline-flex items-center justify-center",
          "size-7 rounded-sm",
          "bg-card/80 backdrop-blur-[2px] border border-border",
          "text-muted-foreground hover:text-foreground hover:bg-card",
          "transition-colors",
          copied && "text-[var(--retro-green)] hover:text-[var(--retro-green)]",
        )}
      >
        <AnimatePresence mode="wait" initial={false}>
          <motion.span
            key={copied ? "check" : "copy"}
            initial={{ opacity: 0, scale: 0.8 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.8 }}
            transition={{ duration: 0.12 }}
            className="inline-flex"
          >
            {copied ? (
              <Check className="size-3.5" strokeWidth={2.5} />
            ) : (
              <Copy className="size-3.5" />
            )}
          </motion.span>
        </AnimatePresence>
      </button>
    </div>
  );
}
