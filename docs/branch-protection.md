# Recommended branch protection

Apply these controls to `main` in GitHub. They are documentation only; Phase 0 does not mutate organization or repository policy.

- Require a pull request before merging.
- Require at least two approvals for financial, security, policy, provider, and production changes; require one approval for other changes.
- Dismiss stale approvals when new commits are pushed.
- Require review from Code Owners.
- Require all jobs in `.github/workflows/ci.yml` to pass.
- Require conversation resolution and a linear history.
- Block force pushes and branch deletion.
- Do not allow bypass for automation identities.
- Keep merge queue or human merge as the final action; never enable automatic Codex merge.
- Protect production environments with named human reviewers and separate secrets.

The initial CODEOWNERS file names the repository owner as a placeholder. A human must replace or expand ownership before Phase 1.
