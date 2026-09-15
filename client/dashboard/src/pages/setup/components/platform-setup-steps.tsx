import { useEffect, useState } from "react";
import { AlertCircle, Check, Copy, Loader2 } from "lucide-react";
import { codeToHtml, type BundledLanguage } from "shiki";
import { Button } from "@/components/ui/Button";
import { Link } from "@/components/ui/Link";
import type { PlatformSetupStep } from "../types";
import { usePlatformPlaceholders } from "./platform-setup-values";

function HighlightedCode({
  code,
  language,
}: {
  code: string;
  language?: string;
}): JSX.Element {
  const [html, setHtml] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    // Drop the previous highlight immediately: shiki resolves asynchronously,
    // and holding the old HTML until it does leaves one platform's snippet on
    // screen under another's heading.
    setHtml(null);
    codeToHtml(code, {
      lang: (language as BundledLanguage) ?? "text",
      theme: "github-dark-default",
      transformers: [
        {
          pre(node) {
            node.properties.class =
              "px-3 pb-3 text-[13px] leading-relaxed whitespace-pre-wrap break-all max-h-[400px] overflow-y-auto !bg-transparent";
          },
        },
      ],
    })
      .then((out) => {
        if (!cancelled) setHtml(out);
      })
      .catch(() => {
        if (!cancelled) setHtml(null);
      });
    return () => {
      cancelled = true;
    };
  }, [code, language]);

  if (html) {
    return (
      <div
        className="text-zinc-200"
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  }
  return (
    <pre className="max-h-[400px] overflow-y-auto px-3 pb-3 text-[13px] leading-relaxed break-all whitespace-pre-wrap">
      <code className="text-zinc-200">{code}</code>
    </pre>
  );
}

interface PlatformSetupStepBodyProps {
  step: PlatformSetupStep;
  /** Small label above the title, e.g. "Step 2". */
  eyebrow: string;
  apiKey?: string;
  apiKeyPending?: boolean;
  apiKeyError?: string;
  onRetryApiKey: () => void;
  onEligibilityAnswer: (eligible: boolean) => void;
}

// One instruction in a platform's setup: the screenshot, prose, help link,
// snippet and — for a platform that first has to establish it qualifies — the
// eligibility question. Shared by the instrumentation sheet, which shows one
// at a time, and by the setup card sections, which stack them.
export function PlatformSetupStepBody({
  step,
  eyebrow,
  apiKey,
  apiKeyPending = false,
  apiKeyError,
  onRetryApiKey,
  onEligibilityAnswer,
}: PlatformSetupStepBodyProps): JSX.Element {
  const { snippetFor, linkFor } = usePlatformPlaceholders();
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);

  const snippet = snippetFor(step, apiKey);

  return (
    <div className="space-y-3">
      <p className="text-muted-foreground text-[11px] font-medium tracking-wider uppercase">
        {eyebrow}
      </p>
      <h4 className="text-foreground text-base font-medium">{step.title}</h4>
      {step.screenshot && (
        <figure className="border-border !my-6 overflow-hidden border">
          <img
            src={step.screenshot.src}
            alt={step.screenshot.alt}
            className="w-full"
          />
          {step.screenshot.caption && (
            <figcaption className="border-border bg-secondary/40 text-muted-foreground border-t px-3 py-2 text-xs leading-relaxed">
              {step.screenshot.caption}
            </figcaption>
          )}
        </figure>
      )}
      {step.description && (
        <p className="text-muted-foreground text-sm leading-relaxed">
          {step.description}
        </p>
      )}

      {step.helpLink &&
        (() => {
          const { url, linkLabel, sentence } = step.helpLink;
          const [before, after] = sentence.split("{LINK}", 2);
          return (
            <p className="text-muted-foreground text-sm leading-relaxed">
              {before}
              <Link
                href={linkFor(url)}
                target="_blank"
                rel="noopener noreferrer"
                size="sm"
                iconSuffixName="external-link"
              >
                {linkLabel}
              </Link>
              {after}
            </p>
          );
        })()}

      {step.eligibility && (
        <div className="bg-secondary/40 border-border !mt-6 space-y-4 border p-4">
          <p className="text-foreground text-sm font-medium">
            {step.eligibility.question}
          </p>
          <div className="flex gap-2">
            <Button
              variant="primary"
              size="sm"
              className="flex-1"
              onClick={() => onEligibilityAnswer(true)}
            >
              <Button.Text>{step.eligibility.yesLabel ?? "Yes"}</Button.Text>
            </Button>
            <Button
              variant="secondary"
              size="sm"
              className="flex-1"
              onClick={() => onEligibilityAnswer(false)}
            >
              <Button.Text>{step.eligibility.noLabel ?? "No"}</Button.Text>
            </Button>
          </div>
        </div>
      )}

      {step.requiresApiKey && apiKeyPending && (
        <div className="text-muted-foreground flex items-center gap-2 text-xs">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Generating API key…
        </div>
      )}

      {step.requiresApiKey && apiKeyError && (
        <div className="text-destructive bg-destructive/5 border-destructive/20 flex items-start gap-2 border p-2.5 text-xs">
          <AlertCircle className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
          <div className="flex-1">
            <p className="font-medium">Couldn't generate an API key</p>
            <p className="text-muted-foreground mt-0.5">{apiKeyError}</p>
            <button
              type="button"
              className="text-foreground mt-1.5 underline underline-offset-2"
              onClick={onRetryApiKey}
            >
              Retry
            </button>
          </div>
        </div>
      )}

      {snippet && (
        <div className="overflow-hidden bg-zinc-950">
          <div className="flex items-center justify-between px-3 py-2.5">
            <span className="text-[10px] tracking-wider text-zinc-500 uppercase">
              {step.language ?? "shell"}
            </span>
            <button
              type="button"
              onClick={() => {
                void navigator.clipboard.writeText(snippet);
                setCopied(true);
              }}
              className="flex items-center gap-1 px-2 py-1 text-[11px] font-medium tracking-wider text-zinc-300 uppercase transition-colors hover:bg-zinc-800 hover:text-zinc-100"
            >
              {copied ? (
                <Check className="h-3 w-3" />
              ) : (
                <Copy className="h-3 w-3" />
              )}
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <HighlightedCode code={snippet} language={step.language} />
        </div>
      )}
    </div>
  );
}
