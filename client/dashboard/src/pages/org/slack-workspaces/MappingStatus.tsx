import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { useIdentityTint } from "@/components/gradient-colors";

export function PersonnelAvatar({
  name,
  email,
  photoUrl,
}: {
  name: string;
  email: string;
  photoUrl?: string;
}): JSX.Element {
  const label = name || email;
  const tint = useIdentityTint(label);
  const initials = label
    .trim()
    .split(/\s+/)
    .map((part) => part[0])
    .slice(0, 2)
    .join("")
    .toUpperCase();
  return (
    <Avatar className="size-6">
      {photoUrl && <AvatarImage src={photoUrl} alt="" />}
      <AvatarFallback className="text-xs" style={tint}>
        {initials || "?"}
      </AvatarFallback>
    </Avatar>
  );
}
