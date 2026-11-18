import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import {
  CLIENT_TYPES,
  clientTypeHint,
  clientTypeLabel,
} from "@/lib/auth-clients";
import type { ClientTypeAccess, ClientTypeRules } from "./roleAuthClientTypes";

const OPTIONS: { value: ClientTypeAccess; label: string }[] = [
  { value: "unset", label: "Not set" },
  { value: "allow", label: "Can use" },
  { value: "block", label: "Blocked" },
];

/**
 * Which kinds of upstream credential the role's holders may use, on every
 * server. Server access is still decided separately, and a credential's own
 * rules can add to these.
 */
export function RoleAuthClientTypesField({
  value,
  onChange,
  disabled,
}: {
  value: ClientTypeRules;
  onChange: (next: ClientTypeRules) => void;
  disabled?: boolean;
}): JSX.Element {
  return (
    <div className="border-border space-y-3 border-t pt-4">
      <div className="space-y-1">
        <Text variant="body" className="font-medium">
          Upstream credentials
        </Text>
        <Text muted small className="block">
          Which kinds of credential people in this role may use to reach
          upstream services. Blocked outranks every other rule.
        </Text>
      </div>
      <ul className="border-border divide-y border">
        {CLIENT_TYPES.map((type) => (
          <li
            key={type}
            className="flex flex-wrap items-center justify-between gap-3 px-3 py-2"
          >
            <div className="min-w-0 flex-1">
              <span className="block text-sm font-medium">
                {clientTypeLabel(type)}
              </span>
              <Text muted small className="block">
                {clientTypeHint(type)}
              </Text>
            </div>
            <SegmentedControl<ClientTypeAccess>
              ariaLabel={`${clientTypeLabel(type)} for this role`}
              value={value[type] ?? "unset"}
              disabled={disabled}
              onChange={(access) => onChange({ ...value, [type]: access })}
              options={OPTIONS}
            />
          </li>
        ))}
      </ul>
    </div>
  );
}
