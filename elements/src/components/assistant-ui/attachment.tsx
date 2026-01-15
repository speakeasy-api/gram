'use client'

import { PropsWithChildren, useEffect, useState, type FC } from 'react'
import { XIcon, PlusIcon, FileText } from 'lucide-react'
import {
  AttachmentPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  useAssistantState,
  useAssistantApi,
} from '@assistant-ui/react'
import { useShallow } from 'zustand/shallow'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Avatar, AvatarImage, AvatarFallback } from '@/components/ui/avatar'
import { TooltipIconButton } from '@/components/assistant-ui/tooltip-icon-button'
import { cn } from '@/lib/utils'

const useFileSrc = (file: File | undefined) => {
  const [src, setSrc] = useState<string | undefined>(undefined)

  useEffect(() => {
    if (!file) {
      setSrc(undefined)
      return
    }

    const objectUrl = URL.createObjectURL(file)
    setSrc(objectUrl)

    return () => {
      URL.revokeObjectURL(objectUrl)
    }
  }, [file])

  return src
}

const useAttachmentSrc = () => {
  const { file, src } = useAssistantState(
    useShallow(({ attachment }): { file?: File; src?: string } => {
      if (attachment.type !== 'image') return {}
      if (attachment.file) return { file: attachment.file }
      const src = attachment.content?.filter((c) => c.type === 'image')[0]
        ?.image
      if (!src) return {}
      return { src }
    })
  )

  return useFileSrc(file) ?? src
}

type AttachmentPreviewProps = {
  src: string
}

const AttachmentPreview: FC<AttachmentPreviewProps> = ({ src }) => {
  const [isLoaded, setIsLoaded] = useState(false)
  return (
    <img
      src={src}
      alt="Image Preview"
      className={
        isLoaded
          ? 'aui-attachment-preview-image-loaded gramel:block gramel:h-auto gramel:max-h-[80vh] gramel:w-auto gramel:max-w-full gramel:object-contain'
          : 'aui-attachment-preview-image-loading gramel:hidden'
      }
      onLoad={() => setIsLoaded(true)}
    />
  )
}

const AttachmentPreviewDialog: FC<PropsWithChildren> = ({ children }) => {
  const src = useAttachmentSrc()

  if (!src) return children

  return (
    <Dialog>
      <DialogTrigger
        className="aui-attachment-preview-trigger gramel:hover:bg-accent/50 gramel:cursor-pointer gramel:transition-colors"
        asChild
      >
        {children}
      </DialogTrigger>
      <DialogContent className="aui-attachment-preview-dialog-content gramel:[&_svg]:text-background gramel:[&>button]:bg-foreground/60 gramel:[&>button]:hover:[&_svg]:text-destructive gramel:p-2 gramel:sm:max-w-3xl gramel:[&>button]:rounded-full gramel:[&>button]:p-1 gramel:[&>button]:opacity-100 gramel:[&>button]:!ring-0">
        <DialogTitle className="aui-sr-only gramel:sr-only">
          Image Attachment Preview
        </DialogTitle>
        <div className="aui-attachment-preview gramel:bg-background gramel:relative gramel:mx-auto gramel:flex gramel:max-h-[80dvh] gramel:w-full gramel:items-center gramel:justify-center gramel:overflow-hidden">
          <AttachmentPreview src={src} />
        </div>
      </DialogContent>
    </Dialog>
  )
}

const AttachmentThumb: FC = () => {
  const isImage = useAssistantState(
    ({ attachment }) => attachment.type === 'image'
  )
  const src = useAttachmentSrc()

  return (
    <Avatar className="aui-attachment-tile-avatar gramel:h-full gramel:w-full gramel:rounded-none">
      <AvatarImage
        src={src}
        alt="Attachment preview"
        className="aui-attachment-tile-image gramel:object-cover"
      />
      <AvatarFallback delayMs={isImage ? 200 : 0}>
        <FileText className="aui-attachment-tile-fallback-icon gramel:text-muted-foreground gramel:size-8" />
      </AvatarFallback>
    </Avatar>
  )
}

const AttachmentUI: FC = () => {
  const api = useAssistantApi()
  const isComposer = api.attachment.source === 'composer'

  const isImage = useAssistantState(
    ({ attachment }) => attachment.type === 'image'
  )
  const typeLabel = useAssistantState(({ attachment }) => {
    const type = attachment.type
    switch (type) {
      case 'image':
        return 'Image'
      case 'document':
        return 'Document'
      case 'file':
        return 'File'
      default: {
        const _exhaustiveCheck: never = type
        throw new Error(`Unknown attachment type: ${_exhaustiveCheck}`)
      }
    }
  })

  return (
    <Tooltip>
      <AttachmentPrimitive.Root
        className={cn('aui-attachment-root gramel:relative',
          isImage &&
            'aui-attachment-root-composer gramel:only:[&>#attachment-tile]:size-24'
        )}
      >
        <AttachmentPreviewDialog>
          <TooltipTrigger asChild>
            <div
              className={cn('aui-attachment-tile gramel:bg-muted gramel:size-14 gramel:cursor-pointer gramel:overflow-hidden gramel:rounded-[14px] gramel:border gramel:transition-opacity gramel:hover:opacity-75',
                isComposer &&
                  'aui-attachment-tile-composer gramel:border-foreground/20'
              )}
              role="button"
              id="attachment-tile"
              aria-label={`${typeLabel} attachment`}
            >
              <AttachmentThumb />
            </div>
          </TooltipTrigger>
        </AttachmentPreviewDialog>
        {isComposer && <AttachmentRemove />}
      </AttachmentPrimitive.Root>
      <TooltipContent side="top">
        <AttachmentPrimitive.Name />
      </TooltipContent>
    </Tooltip>
  )
}

const AttachmentRemove: FC = () => {
  return (
    <AttachmentPrimitive.Remove asChild>
      <TooltipIconButton
        tooltip="Remove file"
        className="aui-attachment-tile-remove gramel:text-muted-foreground gramel:hover:[&_svg]:text-destructive gramel:absolute gramel:top-1.5 gramel:right-1.5 gramel:size-3.5 gramel:rounded-full gramel:bg-white gramel:opacity-100 gramel:shadow-sm gramel:hover:!bg-white gramel:[&_svg]:text-black"
        side="top"
      >
        <XIcon className="aui-attachment-remove-icon gramel:size-3 gramel:dark:stroke-[2.5px]" />
      </TooltipIconButton>
    </AttachmentPrimitive.Remove>
  )
}

export const UserMessageAttachments: FC = () => {
  return (
    <div className="aui-user-message-attachments-end gramel:col-span-full gramel:col-start-1 gramel:row-start-1 gramel:flex gramel:w-full gramel:flex-row gramel:justify-end gramel:gap-2">
      <MessagePrimitive.Attachments components={{ Attachment: AttachmentUI }} />
    </div>
  )
}

export const ComposerAttachments: FC = () => {
  return (
    <div className="aui-composer-attachments gramel:mb-2 gramel:flex gramel:w-full gramel:flex-row gramel:items-center gramel:gap-2 gramel:overflow-x-auto gramel:px-1.5 gramel:pt-0.5 gramel:pb-1 empty:hidden">
      <ComposerPrimitive.Attachments
        components={{ Attachment: AttachmentUI }}
      />
    </div>
  )
}

export const ComposerAddAttachment: FC = () => {
  return (
    <ComposerPrimitive.AddAttachment asChild>
      <TooltipIconButton
        tooltip="Add Attachment"
        side="top"
        variant="ghost"
        size="icon"
        align="start"
        className="aui-composer-add-attachment gramel:hover:bg-muted-foreground/15 gramel:dark:border-muted-foreground/15 gramel:dark:hover:bg-muted-foreground/30 gramel:size-[34px] gramel:rounded-full gramel:p-1 gramel:text-xs gramel:font-semibold"
        aria-label="Add Attachment"
      >
        <PlusIcon className="aui-attachment-add-icon gramel:size-5 gramel:stroke-[1.5px]" />
      </TooltipIconButton>
    </ComposerPrimitive.AddAttachment>
  )
}
