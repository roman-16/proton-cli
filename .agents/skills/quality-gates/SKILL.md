---
name: quality-gates
description: Run code quality checks before committing or when verifying the project builds cleanly. Executes lint, build, and test steps in order. Use after making code changes to ensure nothing is broken.
---

# Quality Gates

Run these commands in order. Stop on the first failure and fix the issue before continuing.

1. **Lint** — check code style and static analysis:
   ```bash
   just lint
   ```

2. **Build** — compile the project:
   ```bash
   just build
   ```

3. **Test** — run the test suite:
   ```bash
   just test
   ```
