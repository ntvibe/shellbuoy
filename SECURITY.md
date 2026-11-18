# Security policy — early alpha

ShellBuoy executes commands as its operating-system user. This is inherently sensitive.

## Non-negotiable boundaries

1. Workspace is **only a working directory**. It does not prevent a command from reading or modifying other files accessible to that user.
2. Every command requires local approval. Inspect the exact executable and arguments before clicking Approve. Python, PowerShell, Bash or similar interpreters can run arbitrary code.
3. The dashboard stays bound to loopback port 3997. **Never expose that port** through Funnel, a reverse proxy or a router.
4. Expose only bearer-protected MCP port 3998, after independently verifying authentication and approval. The random token is not a substitute for future OAuth onboarding.
5. Keep the MCP credential private. Rotate if leaked; client support for bearer headers varies.
6. Funnel creates a **public HTTPS endpoint**. Tailscale connectivity does not itself authenticate MCP requests.
7. Logs may contain sensitive commands, paths and output. They are stored locally and are not encrypted at rest.
8. A permitted command can spawn children; the 120-second timeout is not complete process-tree containment on all platforms.

## Reporting vulnerabilities

Do not post exploits or credentials in public issues. Use GitHub's private vulnerability reporting mechanism if enabled, or contact a maintainer privately.

## Not production hardened

No audited sandboxing, OAuth client registration, per-command path isolation, native OS entitlement model, remote login UI, PTY, complete process-tree termination, log encryption or release signing. This is a prototype for contributors, not a hardened remote administration product.
