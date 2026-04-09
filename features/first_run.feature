Feature: First-run setup

  As a developer installing mosaic on a fresh machine
  I want `mosaic doctor` to bootstrap the data directory and verify
  the environment in a single command
  So that I can be productive immediately and re-running it is safe.

  Scenario: doctor reports a clean environment
    Given a clean mosaic data directory
    When I run "mosaic doctor"
    Then it should exit with status 0
    And the output should contain "data dir"
    And the output should contain "tmux"
    And the output should contain "git"
    And the output should contain "sqlite"
    And the data directory should exist
    And the SQLite database should exist

  Scenario: Migrations are idempotent
    Given a clean mosaic data directory
    When I run "mosaic doctor"
    And I run "mosaic doctor"
    Then it should exit with status 0
    And the schema_migrations table should contain version 1 exactly once

  Scenario: Project config overrides global config
    Given a clean mosaic data directory
    And a global config that sets default_tool to "shell"
    And a project config that sets default_tool to "claude"
    When I run "mosaic doctor --print-config"
    Then it should exit with status 0
    And the output should contain "default_tool"
    And the output should contain "claude"
