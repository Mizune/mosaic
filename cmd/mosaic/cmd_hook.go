package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/hook"
)

// newHookCmd builds the `mosaic hook ...` subtree:
//
//	mosaic hook install   - merges mosaic-owned commands into ~/.claude/settings.json
//	mosaic hook uninstall - removes them again
//	mosaic hook status    - reports whether mosaic is installed
//	mosaic hook recv      - reads stdin (a Claude Code hook payload) and forwards it to mosaic's socket
//
// `recv` is a fallback that lets users wire mosaic into their own
// hooks without going through `nc -U`.
func newHookCmd() *cobra.Command {
	var settingsPath string
	var toolName string

	root := &cobra.Command{
		Use:   "hook",
		Short: "Manage agent hook integration",
	}

	resolvePath := func() string {
		if settingsPath != "" {
			return settingsPath
		}
		home, _ := os.UserHomeDir()
		if toolName == "codex" {
			return filepath.Join(home, ".codex", "hooks.json")
		}
		return filepath.Join(home, ".claude", "settings.json")
	}

	hookSet := func() map[hook.EventName]string {
		if toolName == "codex" {
			return hook.CodexHookSet()
		}
		return hook.MosaicHookSet()
	}

	root.PersistentFlags().StringVar(&settingsPath, "settings", "", "Override the hook settings path (testing)")
	root.PersistentFlags().StringVar(&toolName, "tool", "claude", "Agent tool to manage hooks for (claude or codex)")

	root.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install mosaic hooks into Claude Code's settings.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst := hook.NewInstaller(resolvePath())
			set := hookSet()
			var err error
			if toolName == "codex" {
				err = inst.InstallSetSync(set)
			} else {
				err = inst.InstallSet(set)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %d mosaic hook commands into %s\n",
				len(set), inst.Path)
			return nil
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove mosaic hooks from Claude Code's settings.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst := hook.NewInstaller(resolvePath())
			if err := inst.Uninstall(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed mosaic hooks from %s\n", inst.Path)
			return nil
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Report whether mosaic hooks are installed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			inst := hook.NewInstaller(resolvePath())
			ok, err := inst.IsInstalled()
			if err != nil {
				return err
			}
			if ok {
				fmt.Fprintf(cmd.OutOrStdout(), "✓ mosaic hooks installed in %s\n", inst.Path)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "✗ mosaic hooks NOT installed in %s\n", inst.Path)
			}
			return nil
		},
	})

	return root
}
