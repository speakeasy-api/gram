// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import { Spinner } from "@/components/ui/Spinner";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/Button";
import { SaveIcon } from "lucide-react";
import {
  createContext,
  type ComponentProps,
  use,
  useMemo,
  useState,
} from "react";
import { useSettingsSectionHeading } from "./settings-layout";

type SettingsSectionTone = "default" | "danger";
type SettingsSectionContextValue = {
  tone: SettingsSectionTone;
  /** The section's root, where the settings index links to it. */
  element: HTMLElement | null;
};
type SettingsSectionSlotProps = {
  children?: React.ReactNode;
  className?: string;
};

const SettingsSectionContext = createContext<SettingsSectionContextValue>({
  tone: "default",
  element: null,
});

function SettingsSectionShell({
  children,
  id,
  tone,
}: {
  children: React.ReactNode;
  id?: string;
  tone: SettingsSectionTone;
}) {
  const [element, setElement] = useState<HTMLElement | null>(null);
  const context = useMemo(() => ({ tone, element }), [tone, element]);

  return (
    <SettingsSectionContext.Provider value={context}>
      <section ref={setElement} id={id} className="space-y-3 scroll-mt-4">
        {children}
      </section>
    </SettingsSectionContext.Provider>
  );
}

function SettingsSectionRoot(props: {
  children: React.ReactNode;
  id?: string;
}) {
  return <SettingsSectionShell {...props} tone="default" />;
}

function DangerSettingsSectionRoot(props: {
  children: React.ReactNode;
  id?: string;
}) {
  return <SettingsSectionShell {...props} tone="danger" />;
}

function SettingsSectionHeader({
  children,
  className,
}: SettingsSectionSlotProps) {
  return <div className={cn("space-y-1", className)}>{children}</div>;
}

function SettingsSectionTitle({
  children,
  className,
}: SettingsSectionSlotProps) {
  const { tone, element } = use(SettingsSectionContext);
  // The settings index lists the section under the words its title shows.
  useSettingsSectionHeading(textOf(children).trim(), element);

  return (
    <Heading
      variant="h4"
      className={cn(
        "normal-case",
        tone === "danger" && "text-destructive",
        className,
      )}
    >
      {children}
    </Heading>
  );
}

/** The plain text of a title, leaving out badges and other elements. */
function textOf(node: React.ReactNode): string {
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  return "";
}

function SettingsSectionDescription({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    <Text muted small className={cn("max-w-3xl", className)}>
      {children}
    </Text>
  );
}

function SettingsSectionPanel({
  children,
  className,
}: SettingsSectionSlotProps) {
  const { tone } = use(SettingsSectionContext);

  return (
    <div
      className={cn(
        "overflow-hidden border bg-card",
        tone === "danger" && "border-destructive/30",
        className,
      )}
    >
      {children}
    </div>
  );
}

function SettingsSectionBody({
  children,
  className,
}: SettingsSectionSlotProps) {
  return <div className={cn("space-y-4 p-6", className)}>{children}</div>;
}

function SettingsSectionFooter({
  children,
  className,
}: SettingsSectionSlotProps) {
  const { tone } = use(SettingsSectionContext);

  return (
    <div
      className={cn(
        "flex min-h-[56px] items-center justify-between gap-4 border-t px-6 py-3",
        tone === "danger" ? "bg-destructive/5" : "bg-muted/30",
        className,
      )}
    >
      {children}
    </div>
  );
}

function SettingsSectionFooterHint({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    <Text muted small className={className}>
      {children}
    </Text>
  );
}

function SettingsSectionFooterActions({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    // `ml-auto` rather than the footer's `justify-between`: with no hint beside
    // them the actions are the only child, and justify-between parks a lone
    // child at the start. They belong on the right whether or not anything is
    // said next to them.
    <div className={cn("ml-auto flex shrink-0 items-center gap-2", className)}>
      {children}
    </div>
  );
}

const settingsSectionSlots = {
  Header: SettingsSectionHeader,
  Title: SettingsSectionTitle,
  Description: SettingsSectionDescription,
  Panel: SettingsSectionPanel,
  Body: SettingsSectionBody,
  Footer: SettingsSectionFooter,
  FooterHint: SettingsSectionFooterHint,
  FooterActions: SettingsSectionFooterActions,
};

export const SettingsSection = Object.assign(
  SettingsSectionRoot,
  settingsSectionSlots,
);
export const DangerSettingsSection = Object.assign(
  DangerSettingsSectionRoot,
  settingsSectionSlots,
);

type FooterSaveButtonProps = Omit<ComponentProps<typeof Button>, "children"> & {
  pending: boolean;
};

export function FooterSaveButton({
  pending,
  ...buttonProps
}: FooterSaveButtonProps): JSX.Element {
  return (
    <Button variant="primary" size="md" {...buttonProps}>
      <Button.LeftIcon>
        {pending ? (
          <Spinner className="mr-0 size-4" />
        ) : (
          <SaveIcon aria-hidden="true" className="size-4" />
        )}
      </Button.LeftIcon>
      <Button.Text>{pending ? "Saving" : "Save"}</Button.Text>
    </Button>
  );
}
