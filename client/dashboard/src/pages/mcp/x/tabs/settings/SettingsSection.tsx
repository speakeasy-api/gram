// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Button } from "@speakeasy-api/moonshine";
import { Loader2, SaveIcon } from "lucide-react";
import { createContext, use } from "react";

type SettingsSectionTone = "default" | "danger";
type SettingsSectionContextValue = {
  tone: SettingsSectionTone;
};
type SettingsSectionSlotProps = {
  children?: React.ReactNode;
  className?: string;
};

const DEFAULT_SETTINGS_SECTION_CONTEXT: SettingsSectionContextValue = {
  tone: "default",
};
const DANGER_SETTINGS_SECTION_CONTEXT: SettingsSectionContextValue = {
  tone: "danger",
};
const SettingsSectionContext = createContext(DEFAULT_SETTINGS_SECTION_CONTEXT);

function SettingsSectionRoot({
  children,
  id,
}: {
  children: React.ReactNode;
  id?: string;
}) {
  return (
    <SettingsSectionContext.Provider value={DEFAULT_SETTINGS_SECTION_CONTEXT}>
      <section id={id} className="space-y-3 scroll-mt-4">
        {children}
      </section>
    </SettingsSectionContext.Provider>
  );
}

function DangerSettingsSectionRoot({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <SettingsSectionContext.Provider value={DANGER_SETTINGS_SECTION_CONTEXT}>
      <section className="space-y-3">{children}</section>
    </SettingsSectionContext.Provider>
  );
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
  const { tone } = use(SettingsSectionContext);

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

function SettingsSectionDescription({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    <Type muted small className={cn("max-w-3xl", className)}>
      {children}
    </Type>
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
        "overflow-hidden rounded-xl border bg-card",
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
    <Type muted small className={className}>
      {children}
    </Type>
  );
}

function SettingsSectionFooterActions({
  children,
  className,
}: SettingsSectionSlotProps) {
  return (
    <div className={cn("flex shrink-0 items-center gap-2", className)}>
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

export function FooterSaveButtonContent({
  pending,
}: {
  pending: boolean;
}): JSX.Element {
  if (pending) {
    return (
      <>
        <Button.LeftIcon>
          <Loader2 className="size-4 animate-spin" />
        </Button.LeftIcon>
        <Button.Text>Saving</Button.Text>
      </>
    );
  }

  return (
    <>
      <Button.LeftIcon>
        <SaveIcon className="size-4" />
      </Button.LeftIcon>
      <Button.Text>Save</Button.Text>
    </>
  );
}

export function RowSaveButtonContent({
  pending,
}: {
  pending: boolean;
}): JSX.Element {
  if (pending) {
    return (
      <Button.LeftIcon>
        <Loader2 className="size-4 animate-spin" />
      </Button.LeftIcon>
    );
  }

  return (
    <Button.LeftIcon>
      <SaveIcon className="size-4" />
    </Button.LeftIcon>
  );
}
