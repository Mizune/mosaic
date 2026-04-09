package main

import (
	"os"

	"github.com/spf13/cobra"
)

// newAttachCmd builds `mosaic attach <name>`. It hands the calling
// terminal to tmux via exec.Cmd.Run; on detach (Ctrl+Q binding from
// the mosaic tmux config) the cobra command returns and the user is
// dropped back to their shell.
func newAttachCmd() *cobra.Command {
	var opts runtimeOptions

	cmd := &cobra.Command{
		Use:   "attach <name>",
		Short: "Attach to the tmux session backing a mosaic session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			tmuxCmd, err := rt.sessions.AttachCommand(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			tmuxCmd.Stdin = os.Stdin
			tmuxCmd.Stdout = os.Stdout
			tmuxCmd.Stderr = os.Stderr
			return tmuxCmd.Run()
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}
