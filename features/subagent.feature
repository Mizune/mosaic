Feature: Sub-agent visibility through transcript watching

  As a developer running multiple Claude sessions in parallel
  I want mosaic to surface what every sub-agent is currently doing
  So that I can quickly tell which one needs my attention.

  Scenario: A spawned sub-agent appears in the watcher event stream
    Given a clean Claude transcript directory for project "demo" session "sess-1"
    And the watcher is started for project "demo" session "sess-1"
    When a sub-agent transcript "agent-deadbeef" is appended with one tool_use
    Then a SubagentSpawned event should arrive within 2 seconds
    And a TranscriptEntry event should arrive within 2 seconds

  Scenario: The state machine reports the running tool
    Given a fresh state machine
    When a tool_use "Read" with id "id1" is observed
    Then the snapshot should report "Read" as the current tool
    When a tool_result for "id1" is observed
    Then the snapshot should report no current tool

  Scenario: Notification flips the agent into waiting_input
    Given a fresh state machine
    When a Notification "Permission required" is observed
    Then the snapshot should be awaiting input
    When a tool_use "Bash" with id "id2" is observed
    Then the snapshot should not be awaiting input
