package skill_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Mizune/mosaic/internal/skill"
)

func seedSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover_FlattensFoldedDescription(t *testing.T) {
	root := t.TempDir()
	seedSkill(t, root, "agent-driven-development", `---
name: agent-driven-development
description: >
  Guide for parallel development of large-scale tasks.
  Use this skill when the user wants to parallelize work
  with multiple agents.
---

# Body
`)

	got, err := skill.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 skill, got %d", len(got))
	}
	s := got[0]
	if s.Name != "agent-driven-development" {
		t.Errorf("name: %q", s.Name)
	}
	want := "Guide for parallel development of large-scale tasks. Use this skill when the user wants to parallelize work with multiple agents."
	if s.Description != want {
		t.Errorf("description mismatch:\nwant: %q\ngot:  %q", want, s.Description)
	}
}

func TestDiscover_SingleLineDescription(t *testing.T) {
	root := t.TempDir()
	seedSkill(t, root, "commit", `---
name: commit
description: Commit staged changes with a conventional message.
---
body
`)
	got, err := skill.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Description != "Commit staged changes with a conventional message." {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDiscover_DirectoryAuthoritativeForName(t *testing.T) {
	root := t.TempDir()
	// The frontmatter says "different-name" but Claude Code invokes
	// the skill by its directory name, so Discover must honour that.
	seedSkill(t, root, "commit", `---
name: different-name
description: Commit staged changes.
---
`)
	got, _ := skill.Discover(root)
	if got[0].Name != "commit" {
		t.Errorf("want commit, got %q", got[0].Name)
	}
}

func TestDiscover_MissingRootIsNil(t *testing.T) {
	got, err := skill.Discover(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestDiscover_SortedByName(t *testing.T) {
	root := t.TempDir()
	seedSkill(t, root, "zeta", `---
name: zeta
description: z
---`)
	seedSkill(t, root, "alpha", `---
name: alpha
description: a
---`)
	seedSkill(t, root, "mike", `---
name: mike
description: m
---`)
	got, _ := skill.Discover(root)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "mike" || got[2].Name != "zeta" {
		t.Errorf("not sorted: %+v", got)
	}
}

func TestDiscover_SkipsDirWithoutSkillMd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, root, "real", `---
name: real
description: real
---`)
	got, _ := skill.Discover(root)
	if len(got) != 1 || got[0].Name != "real" {
		t.Fatalf("expected only 'real', got %+v", got)
	}
}
