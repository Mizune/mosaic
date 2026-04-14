package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/selfupdate"
)

func newUpdateCmd() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install mosaic updates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			u := selfupdate.New(version)
			result, err := u.Check(cmd.Context())
			if err != nil {
				return fmt.Errorf("check for updates: %w", err)
			}

			if !result.UpdateAvail {
				fmt.Fprintf(cmd.OutOrStdout(),
					"mosaic %s is up to date.\n", result.Current)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"Update available: %s → %s\n", result.Current, result.Latest)

			if checkOnly {
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Downloading...")
			if err := u.Apply(cmd.Context(), result.Release); err != nil {
				return fmt.Errorf("apply update: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"Updated to mosaic %s. Restart to use the new version.\n",
				result.Latest)
			return nil
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false,
		"Only check for updates without installing")
	return cmd
}
