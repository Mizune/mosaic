// Package skill discovers Claude Code skills so mosaic can attach
// them to individual sessions. A skill is a directory under
// ~/.claude/skills/<name>/ containing a SKILL.md file whose leading
// YAML-ish frontmatter carries a `name` and a `description`.
//
// The package is intentionally tolerant: a malformed frontmatter
// or a missing description is never a fatal error — the skill is
// still returned with whatever fields survived so the user gets at
// least a name in the TUI.
package skill

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill is one discovered skill.
type Skill struct {
	// Name is the directory name and the primary identifier. It
	// matches what shows up as `/<name>` in the Claude Code slash
	// command menu.
	Name string

	// Description is a single-line, flattened version of the
	// multi-line YAML description block. Empty when the file had
	// none or the frontmatter was malformed.
	Description string

	// Path is the absolute path to the SKILL.md file so the TUI
	// can "open in editor" as a later enhancement.
	Path string

	// Root is the skill root that contributed this result. It lets
	// callers explain precedence when several roots contain the same
	// skill name.
	Root string
}

// Root is one skill discovery root. Label is optional display text;
// Path is the directory containing one subdirectory per skill.
type Root struct {
	Path  string
	Label string
}

// Discover walks the skills root and returns every skill with a
// valid SKILL.md. A missing root is not an error: mosaic on a
// machine without any skills installed simply gets an empty list.
//
// The returned slice is sorted by name.
func Discover(root string) ([]Skill, error) {
	return discoverRoot(root)
}

// DiscoverMany discovers skills from multiple roots. Earlier roots
// win on duplicate skill names, so callers should pass higher
// precedence roots first.
func DiscoverMany(roots ...Root) ([]Skill, error) {
	byName := map[string]Skill{}
	for _, root := range roots {
		skills, err := discoverRoot(root.Path)
		if err != nil {
			return nil, err
		}
		label := root.Label
		if label == "" {
			label = root.Path
		}
		for _, s := range skills {
			if _, exists := byName[s.Name]; exists {
				continue
			}
			s.Root = label
			byName[s.Name] = s
		}
	}
	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func discoverRoot(root string) ([]Skill, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(root, e.Name(), "SKILL.md")
		info, err := os.Stat(skillPath)
		if err != nil || info.IsDir() {
			continue
		}
		s := parseSkillFile(skillPath)
		// Force Name from the directory — the frontmatter name
		// must match but the dir is authoritative for mosaic's
		// purposes (it is what Claude Code actually invokes).
		s.Name = e.Name()
		s.Path = skillPath
		s.Root = root
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseSkillFile reads the YAML-ish frontmatter at the top of
// SKILL.md and extracts `name` and `description`. The "YAML-ish"
// caveat matters: Claude Code skill files use `description: > ...`
// folded block syntax which a strict YAML parser would handle, but
// we only need name + description so a single-purpose line reader
// keeps dependencies minimal.
func parseSkillFile(path string) Skill {
	f, err := os.Open(path)
	if err != nil {
		return Skill{}
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	// Find the opening frontmatter fence.
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return Skill{}
	}

	var (
		s           Skill
		inDesc      bool
		descBuilder strings.Builder
		descIndent  int
	)
	consume := func(line string) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return
		}
		if descBuilder.Len() > 0 {
			descBuilder.WriteByte(' ')
		}
		descBuilder.WriteString(trimmed)
	}
	flushDesc := func() {
		if descBuilder.Len() > 0 {
			s.Description = strings.TrimSpace(descBuilder.String())
			descBuilder.Reset()
		}
		inDesc = false
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}

		if inDesc {
			// A blank line inside a folded block is legitimate
			// whitespace — swallow it and keep collecting.
			if trimmed == "" {
				continue
			}
			// A dedented key-looking line ends the folded block
			// and should be re-processed as a new top-level key.
			if leadingSpaces(line) <= descIndent && strings.Contains(trimmed, ":") &&
				!strings.HasPrefix(trimmed, "-") {
				flushDesc()
				// Fall through to key parsing below.
			} else {
				consume(line)
				continue
			}
		}

		key, value, ok := splitKeyValue(trimmed)
		if !ok {
			continue
		}
		switch key {
		case "name":
			s.Name = strings.TrimSpace(strings.Trim(value, `"'`))
		case "description":
			v := strings.TrimSpace(strings.Trim(value, `"'`))
			// `description: > ...` starts a folded block; an empty
			// value on the same line means the description lives
			// on the following indented lines.
			if v == "" || v == ">" || v == "|" || v == ">-" || v == "|-" {
				inDesc = true
				descIndent = leadingSpaces(line)
				continue
			}
			s.Description = v
		}
	}
	if inDesc {
		flushDesc()
	}
	return s
}

// splitKeyValue returns the key/value halves of a `key: value` line,
// or ok=false for anything that is not a top-level key assignment.
func splitKeyValue(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

// leadingSpaces counts the number of leading space characters on s.
func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}
