import { SettingsSection } from "@/components/detail/settings-section";
import { Text } from "@/components/ui/Text";

/**
 * A titled section of the assistant detail page: the shared settings panel
 * (title and description above a white panel), with an optional action on
 * the right of the title and an empty state.
 */
export function Section({
  id,
  title,
  description,
  children,
  empty,
  isEmpty,
  action,
  footer,
}: {
  /** The anchor readiness links land on. */
  id?: string;
  title: string;
  description?: React.ReactNode;
  children: React.ReactNode;
  empty?: string;
  isEmpty?: boolean;
  action?: React.ReactNode;
  footer?: React.ReactNode;
}): JSX.Element {
  return (
    <SettingsSection id={id}>
      <div className="flex items-end justify-between gap-4">
        <SettingsSection.Header>
          <SettingsSection.Title>{title}</SettingsSection.Title>
          {description ? (
            <SettingsSection.Description>
              {description}
            </SettingsSection.Description>
          ) : null}
        </SettingsSection.Header>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {isEmpty && empty ? (
            <Text small muted>
              {empty}
            </Text>
          ) : (
            children
          )}
        </SettingsSection.Body>
        {footer ? (
          <SettingsSection.Footer>{footer}</SettingsSection.Footer>
        ) : null}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

/** A label on the left, its value or control on the right. */
export function Row({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex min-h-9 items-center justify-between gap-6">
      <Text small muted>
        {label}
      </Text>
      <div className="min-w-0">{children}</div>
    </div>
  );
}
