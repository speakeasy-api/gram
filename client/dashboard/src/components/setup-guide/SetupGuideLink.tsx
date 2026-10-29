import { BookOpen, ExternalLink } from "lucide-react";
import { docsUrl } from "./guideDocs";
import { useSetupGuide, type SetupGuideMatch } from "./useSetupGuide";
import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";

export function SetupGuideAction({
  guide,
  className,
  variant = "link",
}: {
  guide: SetupGuideMatch;
  variant?: "link" | "secondary";
  className?: string;
}): React.JSX.Element | null {
  if (variant === "secondary") {
    if (guide.openGuide)
      return (
        <Button
          variant="secondary"
          size="xs"
          className={className}
          onClick={guide.openGuide}
        >
          <Button.LeftIcon>
            <BookOpen />
          </Button.LeftIcon>
          <Button.Text>Read the guide</Button.Text>
        </Button>
      );
    if (!guide.only) return null;
    return (
      <Button variant="secondary" size="xs" className={className} asChild>
        <a href={docsUrl(guide.only)} target="_blank" rel="noopener noreferrer">
          <BookOpen className="mr-2 size-4" aria-hidden />
          Read the guide
          <ExternalLink className="ml-2 size-3" aria-hidden />
        </a>
      </Button>
    );
  }
  const actionClass = cn(
    "text-link-primary flex items-center gap-1 self-end text-xs font-semibold hover:underline",
    className,
  );
  const label = (
    <>
      <BookOpen className="size-3.5" aria-hidden />
      Read the guide
    </>
  );
  if (guide.openGuide)
    return (
      <button type="button" onClick={guide.openGuide} className={actionClass}>
        {label}
      </button>
    );
  if (!guide.only) return null;
  return (
    <a
      href={docsUrl(guide.only)}
      target="_blank"
      rel="noopener noreferrer"
      className={actionClass}
    >
      {label}
      <ExternalLink className="size-3" aria-hidden />
    </a>
  );
}

export function SetupGuideLink({
  serverUrl,
  className,
  variant,
}: {
  serverUrl: string;
  variant?: "link" | "secondary";
  className?: string;
}): React.JSX.Element | null {
  const guide = useSetupGuide({ serverUrl });
  return guide ? (
    <SetupGuideAction guide={guide} className={className} variant={variant} />
  ) : null;
}
