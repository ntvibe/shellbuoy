# Contributing

The goal is a **small, auditable, cross-platform command bridge**. Resist adding agent frameworks, databases, language-model dependencies, workflow engines or bloated dashboards.

## Development

- Go 1.23+; no third-party Go packages.
- Run **go test ./...** and **go vet ./...**.
- Format with **gofmt -w *.go**.
- Test on your OS before claiming runtime compatibility. Cross-compilation alone is not a native runtime test.

## Pull request checklist

- Explain user-visible behavior and any new permission or security exposure.
- Test authentication, approval and failure paths.
- Keep dashboard-only routes inaccessible from the MCP listener.
- Never commit configs, bearer credentials, user history, test logs or binaries.
- Update README and SECURITY.md for behavioral changes.

## Layout

- main.go: config, history, approval, command execution, Tailscale inspection and startup
- server.go: local dashboard and authenticated MCP endpoints
- web/index.html: embedded UI with no JS build chain
- main_test.go: security/approval tests

Use GitHub issues for design discussions before large refactors. No CLA required for the MIT-licensed project.
