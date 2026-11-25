import { GitBranchIcon, FolderGit2Icon, type LucideIcon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

/**
 * The default development readout in the sidebar footer, just above the user
 * menu: which worktree this dev server is serving and what it has checked out.
 * Several worktrees run their own stacks side by side, so a dashboard tab
 * needs to say which one it is.
 *
 * It sits at the bottom of the chrome, out of the brand's way and away from
 * anything it could push around. Long worktree and branch names truncate;
 * hovering floats the untruncated readout over the page.
 *
 * `branchPrefix` renders just before the branch, so a local dev slot
 * (src/dev/slot.local.tsx) can mark the branch up without rebuilding the frame.
 */
export function DevWorktreeReadout({
  branchPrefix,
}: {
  branchPrefix?: ReactNode;
}): JSX.Element {
  const branch = useGitBranch();

  return (
    <div className="group/worktree relative h-8 group-data-[collapsible=icon]:hidden">
      {/* w-max, not inset-x-0: pinning both edges would hold the box at the
          sidebar's width, so the hover max-width would have nothing to expand
          into and a long branch name would stay truncated. */}
      <div className="group-hover/worktree:border-border group-hover/worktree:bg-card absolute top-0 left-0 z-20 flex w-max max-w-full flex-col gap-0.5 overflow-hidden border border-transparent px-1.5 py-1 font-mono text-[10px] leading-none transition-[max-width] duration-150 group-hover/worktree:max-w-[32rem] group-hover/worktree:shadow-md">
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
 *
 * That constant is baked when the dev server starts, so it is already stale for
 * any page loaded after a checkout — a mounting client asks the plugin for the
 * current branch instead of waiting for HEAD to move again.
 */
function useGitBranch(): string {
  const [branch, setBranch] = useState(__GRAM_DEV_BRANCH__);

  useEffect(() => {
    const hot = import.meta.hot;
    if (!hot) return;
    const onBranch = (next: string) => setBranch(next);
    hot.on(__GRAM_DEV_BRANCH_EVENT__, onBranch);
    hot.send(__GRAM_DEV_BRANCH_ASK__);
    return () => hot.off(__GRAM_DEV_BRANCH_EVENT__, onBranch);
  }, []);

  return branch;
}
