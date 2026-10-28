"""Stream an unbound ID token ONLY to the verifier pipe; never run standalone."""

import os
import sys

# Avoid DNS egress; only the GKE Dataplane V2 metadata IP is allowed.
os.environ["GCE_METADATA_HOST"] = "169.254.169.254"
os.environ["GCE_METADATA_IP"] = "169.254.169.254"
os.environ.pop("GOOGLE_APPLICATION_CREDENTIALS", None)


def request_unbound(fetch_id_token, request, audience):
    # Pinned google-auth 2.62.0: (request, audience, bind_id_token=None).
    return fetch_id_token(request, audience=audience, bind_id_token=False)


def main():
    if sys.stdout.isatty():
        print("refusing to emit identity token to terminal", file=sys.stderr)
        return 1
    try:
        from google.auth.transport.requests import Request
        from google.oauth2 import id_token

        token = request_unbound(id_token.fetch_id_token, Request(), sys.argv[1])
        sys.stdout.write(token)
    except Exception:  # noqa: BLE001 -- Never expose SDK credential-bearing exceptions.
        # SDK/HTTP exceptions can contain credentials. Never print their details.
        print(
            "unbound identity request failed; check SDK support and cluster prerequisites",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
