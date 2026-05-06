import { Info } from "lucide-react";

export function LogoutNotice() {
  return (
    <div
      role="note"
      className="flex items-start gap-3 rounded-md border border-[var(--retro-yellow)]/50 bg-[var(--retro-yellow)]/10 px-4 py-3"
    >
      <Info className="size-4 mt-0.5 shrink-0 text-[var(--retro-orange)]" />
      <div className="text-xs leading-relaxed">
        <span className="font-medium">Heads up:</span> after changing a user's
        role or switching the current user, sign out of Gram and sign back in
        for the change to take effect — Gram caches identity in its{" "}
        <code className="font-mono text-[11px]">auth.info</code> response.
      </div>
    </div>
  );
}
