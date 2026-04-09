Feature: Session lifecycle CRUD

  As a developer using mosaic
  I want to create, list, attach to, and kill named sessions
  So that I can manage many parallel agents from the command line.

  Scenario: Creating a session inserts a row and spawns tmux
    Given a clean mosaic data directory
    When I run "mosaic new feat-auth"
    Then it should exit with status 0
    And the output should contain "created session feat-auth"
    And the SQLite database should exist
    And the sessions table should contain a row named "feat-auth"

  Scenario: Listing sessions shows the row I just created
    Given a clean mosaic data directory
    When I run "mosaic new alpha"
    And I run "mosaic ls"
    Then it should exit with status 0
    And the output should contain "alpha"

  Scenario: Killing a session removes it
    Given a clean mosaic data directory
    When I run "mosaic new doomed"
    And I run "mosaic kill doomed"
    And I run "mosaic ls"
    Then it should exit with status 0
    And the output should contain "no sessions yet"

