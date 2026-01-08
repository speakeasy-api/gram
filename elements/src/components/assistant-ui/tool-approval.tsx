import { Button } from '@/components/ui/button'
import { useDensity } from '@/hooks/useDensity'
import { humanizeToolName } from '@/lib/humanize'
import { cn } from '@/lib/utils'
import { AlertTriangle, Check, X } from 'lucide-react'

interface ToolApprovalProps {
  toolName: string
  args: unknown
  onApprove: () => void
  onDeny: () => void
}

export function ToolApproval({
  toolName,
  args,
  onApprove,
  onDeny,
}: ToolApprovalProps) {
  const d = useDensity()

  return (
    <div
      className={cn(
        'aui-tool-approval-root flex w-full flex-col border border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/50',
        d('rounded-md'),
        d('p-md')
      )}
    >
      <div className={cn('flex items-center', d('gap-sm'), d('mb-sm'))}>
        <AlertTriangle className="size-4 text-amber-500" />
        <span className="text-sm font-medium text-amber-700 dark:text-amber-300">
          Approval Required
        </span>
      </div>

      <p className={cn('text-muted-foreground text-sm', d('mb-sm'))}>
        <span className="text-foreground font-medium">
          {humanizeToolName(toolName)}
        </span>{' '}
        wants to execute with the following arguments:
      </p>

      <pre
        className={cn(
          'bg-background overflow-x-auto border text-xs break-all whitespace-pre-wrap',
          d('rounded-sm'),
          d('p-sm'),
          d('mb-md')
        )}
      >
        {JSON.stringify(args, null, 2)}
      </pre>

      <div className={cn('flex justify-end', d('gap-sm'))}>
        <Button
          variant="outline"
          size="sm"
          onClick={onDeny}
          className="text-destructive hover:bg-destructive/10"
        >
          <X className="mr-1 size-3" />
          Deny
        </Button>
        <Button
          variant="default"
          size="sm"
          onClick={onApprove}
          className="bg-emerald-600 hover:bg-emerald-700"
        >
          <Check className="mr-1 size-3" />
          Approve
        </Button>
      </div>
    </div>
  )
}
