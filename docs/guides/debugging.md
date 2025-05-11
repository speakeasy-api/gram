# Debugging Gram Backend

You can debug the Gram server and Temporal worker in your VSCode-compatible IDE
by adding the following launch configurations to your `.vscode/launch.json`
file:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Server",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/server",
      "args": ["start"],
      "env": {
        "TEMPORAL_DEBUG": "true"
      }
    },
    {
      "name": "Worker",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/server",
      "args": ["worker"],
      "env": {
        "TEMPORAL_DEBUG": "true"
      }
    }
  ]
}
```

The `TEMPORAL_DEBUG` environment variable will disable deadlock detection that
Temporal performs at runtime in a production setting. If this environment
variable is not set, then errors like `PanicError: Potential deadlock detected`
will appear in the logs when paused at breakpoints.

Sometimes it may be more convenient to debug the server and worker as a single
process. You can enable this mode by adding the environment variable
`GRAM_SINGLE_PROCESS` to the server launch configuration as follows:

```diff
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Server",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/server",
      "args": ["start"],
      "env": {
        "TEMPORAL_DEBUG": "true",
+       "GRAM_SINGLE_PROCESS": "true"
      }
    }
  ]
}
```

This mode will allow you to use one debug session and set breakpoints across
server code and workflow code.
