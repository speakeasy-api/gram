import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";
import { Resend } from "resend";

const gram = new Gram({
  envSchema: {
    RESEND_API_KEY: z.string(),
  },
}).tool({
  name: "send_email",
  description: "Send an email using Resend",
  inputSchema: {
    email: z.string(),
    subject: z.string(),
    text: z.string(),
  },
  async execute(ctx, input) {
    const resend = new Resend(ctx.env["RESEND_API_KEY"]);

    try {
      const result = await resend.emails.send({
        from: "Acme <onboarding@resend.dev>",
        to: [input["email"]],
        subject: input["subject"],
        html: `<p>${input["text"]}</p>`,
      });

      return ctx.json({
        success: true,
        id: result.data?.id,
        message: "Email sent successfully",
      });
    } catch (error) {
      return ctx.fail(
        {
          error: "Failed to send email",
          details: error instanceof Error ? error.message : String(error),
        },
        { status: 500 }
      );
    }
  },
});

export default gram;