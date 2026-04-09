package tui

// builtinConductorTemplates returns the set of built-in conductor
// templates available in the wizard's template picker step.
// conductorTemplate is defined in conductor_wizard.go.
func builtinConductorTemplates() []conductorTemplate {
	return []conductorTemplate{
		{
			Name:        "CI Pipeline",
			Description: "build → test + bench → report",
			Tasks: []conductorWizardTask{
				{Name: "build", Prompt: "make build", Tool: "shell"},
				{Name: "test", Prompt: "make test", Tool: "shell", DependsOn: []string{"build"}},
				{Name: "bench", Prompt: "make bench", Tool: "shell", DependsOn: []string{"build"}},
				{Name: "report", Prompt: "echo 'All checks passed'", Tool: "shell", DependsOn: []string{"test", "bench"}},
			},
		},
		{
			Name:        "Multi-Angle Refactor",
			Description: "analyse → split → migrate callers → tests",
			Tasks: []conductorWizardTask{
				{Name: "analyse", Prompt: "Read the codebase and produce a refactor plan in docs/refactor.md", Tool: "claude"},
				{Name: "split", Prompt: "Follow docs/refactor.md — extract the module into its own package", Tool: "claude", DependsOn: []string{"analyse"}},
				{Name: "migrate-callers", Prompt: "Update every import site to use the new package path", Tool: "claude", DependsOn: []string{"split"}},
				{Name: "tests", Prompt: "Add integration tests covering the happy path end-to-end", Tool: "claude", DependsOn: []string{"migrate-callers"}},
			},
		},
		{
			Name:        "Review + Fix",
			Description: "review → fix critical + fix warnings → verify",
			Tasks: []conductorWizardTask{
				{Name: "review", Prompt: "Review the codebase for bugs, security issues, and code quality problems", Tool: "claude"},
				{Name: "fix-critical", Prompt: "Fix all critical issues identified in the review", Tool: "claude", DependsOn: []string{"review"}},
				{Name: "fix-warnings", Prompt: "Fix all warning-level issues identified in the review", Tool: "claude", DependsOn: []string{"review"}},
				{Name: "verify", Prompt: "Run the test suite and summarise remaining issues", Tool: "claude", DependsOn: []string{"fix-critical", "fix-warnings"}},
			},
		},
	}
}
