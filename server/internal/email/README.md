# Transactional email in local development

Delivery is opt-in. The application defaults to the legacy Loops provider; merely
supplying `RESEND_API_KEY` does not switch providers. Repository-wide `mise.toml`
is also used by automation, so Resend defaults are not added there.

To enable Resend locally, merge these nonsecret settings into the existing
`[env]` section of your ignored `mise.local.toml` (do not replace the file):

```toml
GRAM_ENVIRONMENT = "local"
GRAM_EMAIL_PROVIDER = "resend"
GRAM_EMAIL_FROM = "platform@notifications.dev.speakeasy.com"
GRAM_EMAIL_REPLY_TO = "platform@speakeasy.com"
```

Supply `RESEND_API_KEY` securely to the local server and worker processes through
your secret manager or ignored local configuration. Never commit the key or put
it in command-line arguments, logs, or screenshots. Restart both processes after
changing their environment. This setup enables real sends when application flows
trigger email; it does not itself send a test message.

Once these nonsecret settings are configured, the key alone controls local
Resend delivery: an empty key or the placeholder `unset` drops transactional
emails. Published templates and a verified nonproduction sender domain are also
required. Outside `local`, selecting Resend without its key fails startup.

Keep `LOOPS_API_KEY` configured separately if local workflows or best-effort
contact enrollment are needed. Resend does not use `GRAM_EMAIL_TEMPLATE_IDS`.
Switching back to `GRAM_EMAIL_PROVIDER = "loops"` deliberately restores legacy
delivery and requires the Loops key and complete legacy template-ID mapping.
