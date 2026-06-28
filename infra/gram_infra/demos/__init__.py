"""Runnable Pub/Sub demos for the gram_infra convenience layer.

Exposed as console scripts (see ``[project.scripts]`` in pyproject.toml):

    uv run pubsub-demo          # callback API (subscriber.receive)
    uv run pubsub-stream-demo   # async-iterator API (subscriber.stream)

Runs against the local Pub/Sub emulator (set PUBSUB_EMULATOR_HOST).
"""
