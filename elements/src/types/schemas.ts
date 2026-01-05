import { z } from 'zod'

const ContentSchema = z
  .union([
    z.object({
      type: z.literal('text'),
      text: z.string(),
    }),
    z.object({
      type: z.literal('image'),
      data: z.string(),
    }),
  ])
  .and(
    z.object({
      _meta: z
        .object({
          'getgram.ai/mime-type': z.string(),
        })
        .optional(),
    })
  )

export type ToolCallResultContent = z.infer<typeof ContentSchema>

export const ToolCallResultSchema = z
  .object({
    content: z.array(ContentSchema),
  })
  .or(z.undefined())

export type ToolCallResult = z.infer<typeof ToolCallResultSchema>
