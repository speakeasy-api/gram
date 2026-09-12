import { GitBranchIcon, FolderGit2Icon, type LucideIcon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

/**
 * The default development readout in the sidebar's brand row: which worktree
 * this dev server is serving and what it has checked out. Several worktrees run
 * their own stacks side by side, so a dashboard tab needs to say which one it
 * is.
 *
 * The readout never contributes to layout — it is absolutely positioned inside
 * the brand row and clamped to that row's box, so no amount of content can
 * shift the page or spill over the nav beneath. Hovering lifts the clamp on
 * both axes and floats the whole thing over the page, which is how it copes
 * with worktree names, branch names and extra lines that don't fit.
 *
 * `branchPrefix` renders just before the branch, so a local dev slot
 * (src/dev-slot.local.tsx) can mark the branch up without rebuilding the frame.
 */
export function DevWorktreeReadout({
  branchPrefix,
}: {
  branchPrefix?: ReactNode;
}): JSX.Element {
  const branch = useGitBranch();

  return (
    <div className="group/worktree relative flex h-full min-w-0 flex-1 items-center group-data-[collapsible=icon]:hidden">
      <div className="group-hover/worktree:border-border group-hover/worktree:bg-card absolute top-1/2 left-1 z-20 flex max-h-[calc(var(--header-height)-0.5rem)] max-w-[calc(100%-0.25rem)] -translate-y-1/2 flex-col gap-0.5 overflow-hidden border border-transparent px-1.5 py-1 font-mono text-[10px] leading-none transition-[max-width,max-height] duration-150 group-hover/worktree:max-h-[32rem] group-hover/worktree:max-w-[32rem] group-hover/worktree:shadow-md">
        <ReadoutLine Icon={FolderGit2Icon} value={__GRAM_DEV_WORKTREE__} />
        <ReadoutLine
          Icon={GitBranchIcon}
          value={branch}
          prefix={branchPrefix}
        />
      </div>
    </div>
  );
}

/** One line of the readout. Exported so local slots can match the style. */
export function ReadoutLine({
  Icon,
  value,
  prefix,
}: {
  Icon: LucideIcon;
  value: string;
  prefix?: ReactNode;
}): JSX.Element {
  return (
    <span className="text-muted-foreground flex items-center gap-1">
      <Icon className="size-2.5 shrink-0" aria-hidden />
      {prefix ? <span className="shrink-0">{prefix}</span> : null}
      <span className="truncate">{value || "unknown"}</span>
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
