package tui

import (
	"fmt"
	"strings"
)

// renderDAGPreview takes the wizard tasks and renders a layered ASCII
// preview showing the dependency structure. Each task is placed in a
// layer determined by the longest-path-from-root algorithm: tasks with
// no dependencies sit at layer 0, and each subsequent task goes into
// one layer past its deepest dependency.
//
// Output example:
//
//	[layer 0]  build
//	[layer 1]  ├─ test       (after: build)
//	[layer 1]  └─ bench      (after: build)
//	[layer 2]  └─ report     (after: test, bench)
func renderDAGPreview(tasks []conductorWizardTask) string {
	if len(tasks) == 0 {
		return "(no tasks)"
	}

	layers, hasCycle := assignLayers(tasks)

	// Group tasks by layer, preserving definition order within each
	// layer so the output is deterministic.
	maxLayer := 0
	for _, l := range layers {
		if l > maxLayer {
			maxLayer = l
		}
	}
	type entry struct {
		task  conductorWizardTask
		layer int
		cycle bool
	}
	grouped := make([][]entry, maxLayer+1)
	for _, t := range tasks {
		l, ok := layers[t.Name]
		if !ok {
			// Unreachable unless the task is in a cycle.
			l = 0
		}
		grouped[l] = append(grouped[l], entry{task: t, layer: l, cycle: hasCycle[t.Name]})
	}

	var lines []string
	for layer, entries := range grouped {
		for i, e := range entries {
			label := formatLayerLabel(layer)
			prefix := treePrefix(i, len(entries), layer)
			suffix := dependsSuffix(e.task.DependsOn)
			if e.cycle {
				suffix += " (!cycle)"
			}
			lines = append(lines, fmt.Sprintf("  %s  %s%s%s", label, prefix, e.task.Name, suffix))
		}
	}
	return strings.Join(lines, "\n")
}

// assignLayers computes the longest-path-from-root layer for each
// task. It returns a name→layer map and a set of names that could not
// be resolved (indicating a dependency cycle).
func assignLayers(tasks []conductorWizardTask) (map[string]int, map[string]bool) {
	deps := make(map[string][]string, len(tasks))
	for _, t := range tasks {
		deps[t.Name] = t.DependsOn
	}

	layers := make(map[string]int, len(tasks))
	state := make(map[string]int, len(tasks)) // 0=unvisited, 1=visiting, 2=done
	hasCycle := map[string]bool{}

	var resolve func(name string) int
	resolve = func(name string) int {
		if state[name] == 2 {
			return layers[name]
		}
		if state[name] == 1 {
			// Cycle detected — mark it and return 0 to avoid
			// infinite recursion.
			hasCycle[name] = true
			return 0
		}
		state[name] = 1
		maxDep := -1
		for _, d := range deps[name] {
			if _, exists := deps[d]; !exists {
				// Unknown dependency — skip silently; validation
				// catches this earlier.
				continue
			}
			dl := resolve(d)
			if dl > maxDep {
				maxDep = dl
			}
		}
		layer := maxDep + 1
		layers[name] = layer
		state[name] = 2
		return layer
	}

	for _, t := range tasks {
		resolve(t.Name)
	}
	return layers, hasCycle
}

// formatLayerLabel renders the "[layer N]" column.
func formatLayerLabel(layer int) string {
	return fmt.Sprintf("[layer %d]", layer)
}

// treePrefix returns the tree-drawing character for a task within its
// layer group. Root-layer tasks (layer 0) get no prefix. In deeper
// layers the last task in the group gets "└─ " and all others get
// "├─ ".
func treePrefix(index, count, layer int) string {
	if layer == 0 {
		return ""
	}
	if index == count-1 {
		return "└─ "
	}
	return "├─ "
}

// dependsSuffix builds the "(after: x, y)" annotation from a
// dependency list. Returns an empty string for root tasks.
func dependsSuffix(deps []string) string {
	if len(deps) == 0 {
		return ""
	}
	return fmt.Sprintf("  (after: %s)", strings.Join(deps, ", "))
}
