import importlib.util
import inspect
import io
import unittest
from unittest.mock import patch

from request_token import main, request_unbound

try:
    HAS_SDK = importlib.util.find_spec("google.oauth2.id_token") is not None
except ModuleNotFoundError:
    HAS_SDK = False


class RequestTokenTest(unittest.TestCase):
    def test_signature_aware_fake(self):
        request = object()

        # Match the inspected 2.62.0 wheel exactly; no permissive **kwargs.
        def fetch_id_token(request_arg, audience, bind_id_token=None):
            self.assertIs(request_arg, request)
            self.assertEqual(audience, "https://example.invalid/probe")
            self.assertIs(bind_id_token, False)
            return "synthetic-test-value"

        self.assertEqual(
            request_unbound(fetch_id_token, request, "https://example.invalid/probe"),
            "synthetic-test-value",
        )

    @unittest.skipUnless(HAS_SDK, "install pinned requirements for SDK contract check")
    def test_actual_pinned_sdk_contract(self):
        from importlib.metadata import version

        from google.oauth2 import id_token

        self.assertEqual(version("google-auth"), "2.62.0")
        request = object()
        inspect.signature(id_token.fetch_id_token).bind(
            request, audience="https://example.invalid/probe", bind_id_token=False
        )
        # Exercise the real wrapper without metadata/network access.
        with patch.object(id_token, "fetch_id_token_credentials") as credentials:
            credentials.return_value.token = "synthetic-test-value"
            self.assertEqual(
                request_unbound(
                    id_token.fetch_id_token, request, "https://example.invalid/probe"
                ),
                "synthetic-test-value",
            )
            credentials.assert_called_once_with(
                "https://example.invalid/probe", request=request, bind_id_token=False
            )
            credentials.return_value.refresh.assert_called_once_with(request)

    def test_refuses_terminal(self):
        with (
            patch("sys.stdout") as stdout,
            patch("sys.stderr", new_callable=io.StringIO),
        ):
            stdout.isatty.return_value = True
            self.assertEqual(main(), 1)
            stdout.write.assert_not_called()

    @unittest.skipUnless(HAS_SDK, "install pinned requirements for error check")
    def test_sdk_errors_are_sanitized(self):
        with (
            patch("sys.argv", ["request_token.py", "https://example.invalid/probe"]),
            patch("sys.stdout", new_callable=io.StringIO) as stdout,
            patch("sys.stderr", new_callable=io.StringIO) as stderr,
            patch(
                "google.oauth2.id_token.fetch_id_token",
                side_effect=RuntimeError("sensitive-test-value"),
            ),
        ):
            self.assertEqual(main(), 1)
            self.assertEqual(stdout.getvalue(), "")
            self.assertNotIn("sensitive-test-value", stderr.getvalue())
            self.assertIn("unbound identity request failed", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
