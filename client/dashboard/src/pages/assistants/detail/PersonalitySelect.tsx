import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  GENERIC_PERSONALITIES,
  TEAM_PERSONALITIES,
  type Personality,
} from "./personalities";

/**
 * Picks a ready-made personality. Picking one hands its text to the caller,
 * which fills the Personality field; the field stays editable afterwards.
 */
export function PersonalitySelect({
  value,
  onPick,
  disabled,
  placeholder = "Start from a preset",
  className,
}: {
  /** The slug last picked, so the trigger names it. */
  value: string | undefined;
  onPick: (personality: Personality) => void;
  disabled?: boolean;
  placeholder?: string;
  className?: string;
}): JSX.Element {
  const all = [...TEAM_PERSONALITIES, ...GENERIC_PERSONALITIES];
  return (
    <Select
      value={value ?? ""}
      disabled={disabled}
      onValueChange={(slug) => {
        const picked = all.find((p) => p.slug === slug);
        if (picked) onPick(picked);
      }}
    >
      <SelectTrigger className={className} aria-label="Personality preset">
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent className="max-h-[420px]">
        <SelectGroup>
          <SelectLabel>For a team</SelectLabel>
          {TEAM_PERSONALITIES.map((p) => (
            <SelectItem key={p.slug} value={p.slug} description={p.summary}>
              {p.title}
            </SelectItem>
          ))}
        </SelectGroup>
        <SelectGroup>
          <SelectLabel>General</SelectLabel>
          {GENERIC_PERSONALITIES.map((p) => (
            <SelectItem key={p.slug} value={p.slug} description={p.summary}>
              {p.title}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  );
}
