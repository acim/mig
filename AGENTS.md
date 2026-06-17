# Agent Instructions

## Development Workflow

- Use test-driven development for code changes.
- Write a failing test first that captures the intended behavior or regression.
- Implement the smallest scoped change needed to make that test pass.
- Run the relevant tests and confirm the new test is green before considering the change complete.

## Sandbox Notes

- Run Go commands that build or test code outside the sandbox when they need the normal Go build cache. The sandbox can block access to `~/Library/Caches/go-build`, causing noisy `operation not permitted` setup failures unrelated to the code.
- Run Podman commands outside the sandbox. This includes `podman-compose up`, `podman-compose down`, and other container operations used for PostgreSQL integration tests.
- Run GitHub/network CLI commands outside the sandbox. This includes `gh` commands and other commands that need reliable network access.
- If a command fails with a likely sandbox-related filesystem, cache, container, or network error, rerun the same command outside the sandbox before treating it as a project failure.

## Dependencies

- Do not introduce third-party dependencies without human approval.
- When a third-party dependency would be useful, explain why it is the best fit and ask for approval before adding it.
