---
name: quality-gates
description: Run code quality checks before committing or when verifying the project builds cleanly. Executes lint and build. Use after making code changes to ensure nothing is broken. The user runs the test suite manually.
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

3. **Tests** — **do not run.** The test suite (`just test`) hits the live Proton API, creates real data, and takes several minutes. The user runs it manually and reports results back. Your job is to make the code compile cleanly and look right; the user verifies behaviour.
