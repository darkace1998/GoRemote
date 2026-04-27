# Copilot Instructions for This Repository

## Project Context
This repository currently contains planning artifacts for a Go-based, cross-platform successor to mRemoteNG. The core reference docs are:
- `requirements.md` (product and non-functional requirements)
- `architecture.md` (target system architecture and boundaries)
- `stages.md` (delivery sequencing)
- `plan.md` (execution strategy and decision rules)

Use these files as the source of truth when proposing implementation details.

## Build, Test, and Lint Commands
No runnable build, test, or lint commands are currently defined in this repository.

When implementation code is added, document:
- full build/test/lint commands
- single-test invocation format for the test framework in use

## High-Level Architecture
- **Layered architecture:** UI shell (recommended Wails + React/TypeScript) over a Go application core, domain model, persistence layer, protocol host, credential host, and platform services.
- **Strict separation of concerns:** UI handles layout/workspace interactions; app core owns commands/state/events; domain models product concepts; protocol and credential implementations stay out of core business logic.
- **Plugin-first extensibility:** protocol modules and credential providers are designed as built-in or external plugins with a stable contract.
- **Out-of-process plugins:** external protocols/providers should communicate over IPC (gRPC/Connect over named pipes or Unix sockets), not Go's native `plugin` package.
- **Migration as a product feature:** import compatibility from existing mRemoteNG data (XML/CSV, inheritance, folder structure) is a first-class requirement.

## Key Conventions
- Treat this as a **redesign for cross-platform parity**, not a line-by-line Windows-era port.
- Keep package boundaries strong: core/domain/persistence/protocol/credential concerns should remain decoupled.
- Route UI mutations through explicit backend commands; avoid protocol-specific behavior leaking into UI state handling.
- Store credential references in connection definitions whenever possible; avoid persisting raw secrets.
- Use capability-declared plugin manifests with explicit trust/permission boundaries and versioned contracts.
- Prefer explicit import warnings over silent data drops during migration flows.
- Use `context.Context` consistently for cancellation, timeouts, and request scoping in long-running or external operations.
- Preserve release sequencing discipline from `stages.md` (foundation/core first, then protocol breadth and hardening).
