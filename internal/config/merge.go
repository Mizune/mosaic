package config

// Merge layers an override [Config] on top of a base. The base is
// typically the built-in defaults merged with the global config; the
// override is typically a project-local config.
//
// Merge rules:
//
//   - Scalars (strings, ints, bools, floats): the override wins when
//     the override value is non-zero. Zero values in the override are
//     left untouched (the base value carries through).
//   - Maps: each key is recursively merged. Keys present only in the
//     override are added; keys present only in the base are kept.
//   - Lists: the override replaces the base list when it is non-nil
//     and non-empty. Empty/nil lists in the override leave the base
//     value alone. To extend a list rather than replace it, use the
//     companion *_extra fields.
//   - WorktreeSetup: each setup is merged field-by-field; CopyExtra
//     and CommandsExtra are appended to the resolved Copy/Commands
//     after replacement.
//
// Merge does not mutate either argument; it returns a new value.
func Merge(base, override Config) Config {
	out := base
	out.Mosaic = mergeMosaic(base.Mosaic, override.Mosaic)
	out.UI = mergeUI(base.UI, override.UI)
	out.Tmux = mergeTmux(base.Tmux, override.Tmux)
	out.Worktree = mergeWorktree(base.Worktree, override.Worktree)
	out.Hooks = mergeHooks(base.Hooks, override.Hooks)
	out.Discord = mergeDiscord(base.Discord, override.Discord)
	out.Projects = mergeProjects(base.Projects, override.Projects)
	return out
}

// mergeProjects concatenates the two project registries, with the
// override winning on conflicting names. Entries appear in the
// resulting slice in stable order: first every base entry (in the
// order it was declared), then any override-only entries.
func mergeProjects(base, ov []ProjectEntry) []ProjectEntry {
	if len(base) == 0 && len(ov) == 0 {
		return nil
	}
	index := map[string]int{}
	out := make([]ProjectEntry, 0, len(base)+len(ov))
	for _, p := range base {
		if p.Name == "" {
			continue
		}
		index[p.Name] = len(out)
		out = append(out, p)
	}
	for _, p := range ov {
		if p.Name == "" {
			continue
		}
		if i, ok := index[p.Name]; ok {
			// Merge field-by-field so the override can tweak one
			// setting without restating the whole entry.
			if p.Path != "" {
				out[i].Path = p.Path
			}
			if p.DefaultTool != "" {
				out[i].DefaultTool = p.DefaultTool
			}
			if p.DefaultSetup != "" {
				out[i].DefaultSetup = p.DefaultSetup
			}
			if p.DefaultBranch != "" {
				out[i].DefaultBranch = p.DefaultBranch
			}
			continue
		}
		index[p.Name] = len(out)
		out = append(out, p)
	}
	return out
}

func mergeMosaic(base, ov MosaicSection) MosaicSection {
	out := base
	if ov.Profile != "" {
		out.Profile = ov.Profile
	}
	if ov.DataDir != "" {
		out.DataDir = ov.DataDir
	}
	if ov.LogLevel != "" {
		out.LogLevel = ov.LogLevel
	}
	if ov.DefaultTool != "" {
		out.DefaultTool = ov.DefaultTool
	}
	return out
}

func mergeUI(base, ov UISection) UISection {
	out := base
	if ov.Theme != "" {
		out.Theme = ov.Theme
	}
	if ov.PreviewRefreshMs != 0 {
		out.PreviewRefreshMs = ov.PreviewRefreshMs
	}
	if ov.ListWidthRatio != 0 {
		out.ListWidthRatio = ov.ListWidthRatio
	}
	if ov.AutoExpandSubAgent {
		out.AutoExpandSubAgent = true
	}
	return out
}

func mergeTmux(base, ov TmuxSection) TmuxSection {
	out := base
	if ov.SocketName != "" {
		out.SocketName = ov.SocketName
	}
	if ov.SessionPrefix != "" {
		out.SessionPrefix = ov.SessionPrefix
	}
	return out
}

func mergeWorktree(base, ov WorktreeSection) WorktreeSection {
	out := base
	if ov.Root != "" {
		out.Root = ov.Root
	}
	if ov.DefaultBaseBranch != "" {
		out.DefaultBaseBranch = ov.DefaultBaseBranch
	}
	if ov.AutoWorktree {
		out.AutoWorktree = true
	}
	if ov.DefaultSetup != "" {
		out.DefaultSetup = ov.DefaultSetup
	}
	out.Setup = mergeSetupMap(base.Setup, ov.Setup)
	return out
}

func mergeSetupMap(base, ov map[string]WorktreeSetup) map[string]WorktreeSetup {
	if base == nil && ov == nil {
		return nil
	}
	out := make(map[string]WorktreeSetup, len(base)+len(ov))
	for k, v := range base {
		out[k] = v
	}
	for k, ovSetup := range ov {
		if existing, ok := out[k]; ok {
			out[k] = mergeWorktreeSetup(existing, ovSetup)
		} else {
			// Override-only setup: still finalise extras so the
			// resolved Copy/Commands are easy to consume downstream.
			out[k] = finaliseSetup(ovSetup)
		}
	}
	// Finalise base-only setups so callers can read Copy/Commands
	// without re-running the merger.
	for k, baseSetup := range base {
		if _, ok := ov[k]; ok {
			continue
		}
		out[k] = finaliseSetup(baseSetup)
	}
	return out
}

func mergeWorktreeSetup(base, ov WorktreeSetup) WorktreeSetup {
	out := base
	if len(ov.Copy) > 0 {
		out.Copy = append([]string(nil), ov.Copy...)
	}
	if len(ov.CopyExtra) > 0 {
		out.Copy = append(append([]string(nil), out.Copy...), ov.CopyExtra...)
	}
	if ov.CopyIgnored {
		out.CopyIgnored = true
	}
	if ov.CopyUntracked {
		out.CopyUntracked = true
	}
	if len(ov.Commands) > 0 {
		out.Commands = append([]string(nil), ov.Commands...)
	}
	if len(ov.CommandsExtra) > 0 {
		out.Commands = append(append([]string(nil), out.Commands...), ov.CommandsExtra...)
	}
	if ov.PostCreateHook != "" {
		out.PostCreateHook = ov.PostCreateHook
	}
	out.Env = mergeStringMap(base.Env, ov.Env)
	// Drop the *Extra fields from the resolved value: they have already
	// been folded into Copy/Commands.
	out.CopyExtra = nil
	out.CommandsExtra = nil
	return out
}

// finaliseSetup applies CopyExtra/CommandsExtra to a setup that has not
// been merged against another setup. It is the identity transformation
// for the resolved Copy/Commands fields.
func finaliseSetup(s WorktreeSetup) WorktreeSetup {
	out := s
	if len(s.CopyExtra) > 0 {
		out.Copy = append(append([]string(nil), s.Copy...), s.CopyExtra...)
	}
	if len(s.CommandsExtra) > 0 {
		out.Commands = append(append([]string(nil), s.Commands...), s.CommandsExtra...)
	}
	out.CopyExtra = nil
	out.CommandsExtra = nil
	return out
}

func mergeStringMap(base, ov map[string]string) map[string]string {
	if base == nil && ov == nil {
		return nil
	}
	out := make(map[string]string, len(base)+len(ov))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range ov {
		out[k] = v
	}
	return out
}

func mergeDiscord(base, ov DiscordSection) DiscordSection {
	out := base
	if ov.Enabled {
		out.Enabled = true
	}
	if ov.Token != "" {
		out.Token = ov.Token
	}
	if ov.ChannelID != "" {
		out.ChannelID = ov.ChannelID
	}
	if ov.GuildID != "" {
		out.GuildID = ov.GuildID
	}
	if ov.NotifyOnStop {
		out.NotifyOnStop = true
	}
	if ov.NotifyOnAttention {
		out.NotifyOnAttention = true
	}
	if len(ov.AllowedUserIDs) > 0 {
		out.AllowedUserIDs = append([]string(nil), ov.AllowedUserIDs...)
	}
	return out
}

func mergeHooks(base, ov HooksSection) HooksSection {
	out := base
	// Suggested is a bool whose default is true. We treat the override
	// as authoritative whenever the project config explicitly disables
	// or enables it; the simplest interpretation is "if Events is set
	// the user has opinions about hooks, take their Suggested value".
	if len(ov.Events) > 0 {
		out.Events = append([]string(nil), ov.Events...)
		out.Suggested = ov.Suggested
	}
	return out
}
