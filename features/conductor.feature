Feature: Conductor (Master/Worker DAG)

  As a developer with a multi-step refactor in mind
  I want to describe the plan once in TOML and let mosaic schedule
  the tasks across worker sessions
  So that independent steps run in parallel and dependent ones
  wait for their inputs.

  Scenario: dry-run prints the task graph
    Given a clean mosaic data directory
    And a conductor plan with three sequential tasks
    When I run "mosaic conductor run PLAN_FILE --dry-run"
    Then it should exit with status 0
    And the output should contain "conductor: triple"
    And the output should contain "tasks:"
    And the output should contain "scaffold"
    And the output should contain "tests"
    And the output should contain "docs"

  Scenario: invalid plan with a cycle is rejected
    Given a clean mosaic data directory
    And a conductor plan that contains a cycle
    When I run "mosaic conductor run PLAN_FILE --dry-run"
    Then it should exit with status 1
    And the output should contain "cycle"
