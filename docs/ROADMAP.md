# Roadmap

## v0.1 — co-development baseline

- [x] Distinct project and source tree, no Helios runtime dependency
- [x] Cross-platform Go build targets
- [x] Browser dashboard with workspace setup and permission status
- [x] Loopback MCP listener with random bearer credential
- [x] Single command-execution tool with local approval and visible output
- [x] Local append-only command history
- [x] Tailscale CLI detection and guided Funnel instructions
- [x] Copyable AI-chat setup prompt
- [ ] Native runtime and Tailscale tests on Linux and Windows
- [ ] Signed desktop installer for each OS

## v0.2 — client interoperability

- MCP OAuth authorization metadata + protected resource metadata
- PKCE, client registration/metadata, client revoke, token audience/expiry
- Real ChatGPT and Gemini custom MCP connection tests
- Guided Tailscale Funnel start/stop with explicit permissions
- Log export, pagination and command cancellation

## v0.3 — isolation hardening

- Optional platform-specific OS sandbox
- Interactive PTY and streaming flow control
- Process-tree cancellation and output/disk quotas
- Security review, platform signing and release provenance

**Out of scope:** agent orchestration, task scheduling, memory, GUI remote control, durable workflows and embedded AI models.
