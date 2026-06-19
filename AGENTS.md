# Agent Instructions

## Development Workflow

- Use test-driven development for code changes.
- Write a failing test first that captures the intended behavior or regression.
- Implement the smallest scoped change needed to make that test pass.
- Run the relevant tests and confirm the new test is green before considering the change complete.
- Whenever Go code or tests change, run the real coverage command and update the README coverage badge number to match. Keep the existing badge format, including the escaped percent as `%25`.

## Dependencies

- Do not introduce third-party dependencies without human approval.
- When a third-party dependency would be useful, explain why it is the best fit and ask for approval before adding it.

## GitHub Security

- GitHub CodeQL/code scanning default setup is already enabled for this repository for Go and GitHub Actions, with the default query suite on a weekly schedule.
