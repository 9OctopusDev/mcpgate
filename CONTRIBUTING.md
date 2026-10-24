# Contributing to mcpgate

Thanks for helping improve mcpgate. Before starting substantial work, open an issue so maintainers and contributors can agree on the problem and approach.

## Development setup

Install Go 1.27.3 and Docker, then clone the canonical repository:

```sh
git clone https://github.com/9OctopusDev/mcpgate.git
cd mcpgate
go mod download
```

Run the normal development checks with:

```sh
make test
make conformance
make release-gate
```

Run `make container` when changing the Dockerfile, dependency graph, build flags, or startup behavior.

## Pull requests

- Keep each pull request focused and explain its user-visible effect.
- Add tests for behavior changes and reproduce bug fixes with a failing test first.
- Update configuration, operations, security, and design documentation when their contracts change.
- Run the relevant checks locally and report the commands and results in the pull request.
- Never commit credentials, tokens, private endpoints, customer data, or secret-bearing logs.
- Do not commit generated binaries, transient benchmark output, or release reports.

Contributions are accepted under the repository's Apache-2.0 license. The project does not currently require a CLA or DCO sign-off.

Report security vulnerabilities through the private process in [SECURITY.md](SECURITY.md), not through a public issue.
