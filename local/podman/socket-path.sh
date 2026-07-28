#!/usr/bin/env sh
# Prints the DOCKER_HOST URL for the Podman docker-compat API socket.
# Referenced by mise.toml's [env] exec template — keep it fast and side-effect
# free (it runs on every mise env evaluation).
#
# uid 0 (e.g. Cursor Cloud sandboxes): podman runs rootful, system socket path.
# Otherwise: rootless, per-user runtime dir (XDG_RUNTIME_DIR may be unset in
# minimal shells, fall back to /run/user/<uid>).
uid=$(id -u)
if [ "$uid" = "0" ]; then
  printf 'unix:///run/podman/podman.sock'
else
  printf 'unix://%s/podman/podman.sock' "${XDG_RUNTIME_DIR:-/run/user/$uid}"
fi
