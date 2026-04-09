Feature: Worktree creation with declarative setup

  As a developer using mosaic
  I want a new mosaic session to optionally create its own git
  worktree pre-populated with the files my agent needs
  So that I can spin up isolated workspaces in one command.

  Scenario: --worktree creates a new git worktree from main
    Given a clean mosaic data directory
    And a git repository with one commit
    When I run "mosaic new feat-a --path REPO_DIR --worktree --branch feat-a --base main"
    Then it should exit with status 0
    And the output should contain "worktree="
    And the worktree directory should exist for branch "feat-a"

  Scenario: Declarative copy list pulls .env into the new worktree
    Given a clean mosaic data directory
    And a git repository with one commit and a .env file
    When I run "mosaic new env-copy --path REPO_DIR --worktree --branch env-copy --base main"
    Then it should exit with status 0
    And the worktree should contain a copy of .env
