# ShellBuoy

**A tiny, self-hosted MCP bridge for supervised terminal commands.**

ShellBuoy lets an AI client request a command on your computer. The command appears in a local terminal-style dashboard and **cannot execute until you explicitly approve it**. Output and exit status are visible as the process runs and stored locally for later review.

No cloud backend, no embedded model, no multi-agent system, no scheduled jobs. Independently developed as a fresh project.

> Early alpha (v0.1). This is a command executor, not an OS sandbox. Do not expose it to the public internet without understanding the security boundaries below.

## What works today

- macOS, Linux and Windows from one small Go codebase (standard library only).
- Local browser dashboard at http://127.0.0.1:3997; it never listens on a public interface.
- Authenticated MCP endpoint at http://127.0.0.1:3998/mcp (separate port).
- One write-capable MCP tool, **exec_command**, which queues a request; read-only **command_status** retrieves its result.
- Visible approval/denial of every requested command, combined stdout/stderr, exit codes, local JSONL history.
- Workspace selection, credential rotation, a built-in smoke-test command, and Tailscale status.
- Copyable Tailscale setup instructions to share with an AI assistant.
- No automatic Funnel exposure: the user explicitly enables it.

## Start in development

Requires [Go](https://go.dev/dl/) 1.23 or newer.

~~~bash
git clone https://github.com/ntvibe/shellbuoy.git
cd shellbuoy
go run .
~~~

Or build a native executable with **go build -o shellbuoy .** (Windows: **go build -o shellbuoy.exe .**).

1. Open the dashboard on the same computer.
2. Enter an **existing absolute workspace folder** and click **Use this workspace**.
3. Try the smoke-test: executable **git**, argument **--version**. Approve it locally and inspect the output.
4. Connect a supported MCP client with the endpoint and a bearer credential (copy from the local dashboard).
5. Optionally follow the guided Tailscale setup to expose **only the MCP port** over HTTPS.

No accounts or subscriptions are needed to run ShellBuoy locally. For remote access, each user manages their own Tailscale account.

## Tailscale Funnel: bring your own account

Install [Tailscale](https://tailscale.com/download), sign in, and follow the dashboard.

~~~text
tailscale up
tailscale funnel --bg 3998
tailscale funnel status
~~~

Depending on OS and Tailscale packaging, setup may require elevated permission or web authorization. Funnel makes the MCP port **publicly reachable**. ShellBuoy's separate local dashboard on 3997 is **not** proxied. The hostname is provided by Tailscale, usually as https://DEVICE.TAILNET.ts.net/mcp; it depends on the user's tailnet and may change if device or tailnet identity changes.

**Do not run Funnel until you have confirmed token authentication and local approval work.** The v0.1 bearer credential is a secret; if exposed, rotate it.

## MCP contract

ShellBuoy currently speaks stateless HTTP MCP with a bearer header.

~~~http
POST /mcp
Authorization: Bearer YOUR_LOCALLY_GENERATED_SECRET
Content-Type: application/json

{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"exec_command","arguments":{"command":"git","args":["status","--short"]}}}
~~~

The response includes a **pending command ID**, not an executed result. Approve or deny in the local dashboard, then call **command_status** with an ID to retrieve the state, output and exit status.

Only executable + explicit argument arrays are accepted. There is no implicit shell interpreter. Running **sh -c** or **cmd /c** is possible only as an explicit, separately approved request.

## What does NOT work yet

**ChatGPT and Gemini consumer remote OAuth pairing is not implemented.** Those applications may not allow configuring a custom Authorization header. We will add proper MCP OAuth discovery, PKCE, local user approval and client registration before claiming one-click ChatGPT/Gemini interoperability. The existing bearer endpoint is suitable only for compatible MCP clients and controlled tests.

Also absent: OS-level sandboxing, interactive PTY, cancellation of running commands, GUI automation, filesystem MCP tools, multi-agent scheduling, background workflows, and packaged installers. See [ROADMAP](docs/ROADMAP.md).

## Safety model and limitations

- All execution requires visible local approval; there is no “allow all” mode.
- Commands run as the current OS user and **can access files outside the selected workspace**. Working directory is **not** a sandbox or a security boundary.
- Child processes receive a small allowlist of environment variables; that does not guarantee isolation from local secrets.
- Each command has a 120-second limit; captured output is truncated after 256 KiB. Command requests, decisions, output (up to that limit), errors and exit status are stored in local append-only JSONL.
- Config and history live in your user config directory (shellbuoy subfolder); Unix permissions are requested as 0600. Windows security depends on the user's directory ACL.
- Dashboard requests use a CSRF token, Origin/Host checks, and loopback binding. The MCP listener uses a separate loopback port and random 256-bit bearer credential.
- There is **no central ShellBuoy account or hosted relay**. Tailscale may carry remote traffic under its own terms.

Read [SECURITY.md](SECURITY.md) before opening remote access.

## Contributing

Contributions welcome, especially **native Linux and Windows tests**, MCP interoperability, UX and security review. Run **go test ./...**, **go vet ./...**, and **gofmt** before a PR. See [CONTRIBUTING.md](CONTRIBUTING.md).

MIT licensed. ShellBuoy is independent and does not include Helios code, runtime state, configuration or credentials.
