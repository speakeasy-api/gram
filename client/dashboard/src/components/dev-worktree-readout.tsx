import { GitBranchIcon, FolderGit2Icon, type LucideIcon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

/**
 * The default development readout in the sidebar's brand row: which worktree
 * this dev server is serving and what it has checked out. Several worktrees run
 * their own stacks side by side, so a dashboard tab needs to say which one it
 * is.
 *
 * Truncates to the sidebar's width and floats the full readout over the page on
 * hover, since worktree and branch names routinely outrun 16rem.
 *
 * `children` are rendered as extra lines below, so a local dev slot
 * (src/dev-slot.local.tsx) can add its own without rebuilding the frame.
 */
export function DevWorktreeReadout({
  children,
}: {
  children?: ReactNode;
}): JSX.Element {
  const branch = useGitBranch();

  return (
    <div className="group/worktree relative flex h-full min-w-0 flex-1 items-center group-data-[collapsible=icon]:hidden">
      <div className="group-hover/worktree:border-border group-hover/worktree:bg-card absolute left-1 z-20 flex max-w-[calc(100%-0.25rem)] flex-col gap-0.5 overflow-hidden border border-transparent px-1.5 py-1 font-mono text-[10px] leading-none transition-[max-width] duration-150 group-hover/worktree:max-w-[32rem] group-hover/worktree:shadow-md">
        <ReadoutLine Icon={FolderGit2Icon} value={__GRAM_DEV_WORKTREE__} />
        <ReadoutLine Icon={GitBranchIcon} value={branch} />
        {children}
      </div>
    </div>
  );
}

/** One line of the readout. Exported so local slots can match the style. */
export function ReadoutLine({
  Icon,
  value,
  note,
}: {
  Icon: LucideIcon;
  value: string;
  note?: string | undefined;
}): JSX.Element {
  return (
    <span className="text-muted-foreground flex items-center gap-1">
      <Icon className="size-2.5 shrink-0" aria-hidden />
      <span className="truncate">{value || "unknown"}</span>
      {note ? <span className="shrink-0 opacity-60">· {note}</span> : null}
    </span>
  );
}

/**
 * The branch is the one value that moves while the dev server runs, so the
 * plugin pushes a fresh one over HMR whenever HEAD changes rather than letting
 * the baked-in constant go stale after a checkout.
 */
function useGitBranch(): string {
  const [branch, setBranch] = useState(__GRAM_DEV_BRANCH__);

  useEffect(() => {
    const hot = import.meta.hot;
    if (!hot) return;
    const onBranch = (next: string) => setBranch(next);
    hot.on(__GRAM_DEV_BRANCH_EVENT__, onBranch);
    return () => hot.off(__GRAM_DEV_BRANCH_EVENT__, onBranch);
  }, []);

  return branch;
}
