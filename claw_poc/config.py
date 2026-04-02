"""
DefenseClaw x Gram — Configuration

The inputs needed to provision a DefenseClaw-governed OpenClaw instance.
In production, these values come from the Gram dashboard.

Used by:
  - verify_config.py (local sanity check)
  - poc.py (passes these as env vars to the Docker container)
"""
import os
from pathlib import Path

# Load .env.local from repo root if it exists
_env_local = Path(__file__).resolve().parent.parent / ".env.local"
if _env_local.exists():
    for line in _env_local.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            key, _, value = line.partition("=")
            os.environ[key.strip()] = value.strip()

# ---------------------------------------------------------------------------
# Gram API
# ---------------------------------------------------------------------------
GRAM_SERVER_URL = os.environ.get("GRAM_SERVER_URL", "https://app.getgram.ai")
GRAM_PROJECT_SLUG = os.environ.get("GRAM_PROJECT_SLUG", "ryan")
GRAM_API_KEY = os.environ.get("GRAM_API_KEY", "")
