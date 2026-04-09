package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/Mizune/mosaic/internal/store"
)

// newListCmd builds `mosaic ls`. The output is a tab-aligned table
// with one row per session, sorted most-recently-active first. The
// command also prints a hint when the live tmux state and the
// database disagree (i.e. a session crashed outside mosaic).
func newListCmd() *cobra.Command {
	var opts runtimeOptions

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List mosaic sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := openRuntime(cmd.Context(), opts)
			if err != nil {
				return err
			}
			defer rt.Close()

			res, err := rt.sessions.List(cmd.Context())
			if err != nil {
				return err
			}
			renderSessions(cmd.OutOrStdout(), res.Sessions)
			if len(res.TmuxMissing) > 0 {
				fmt.Fprintf(cmd.OutOrStderr(),
					"\n  ! tmux session missing for: %s (status flipped to stopped)\n",
					strings.Join(res.TmuxMissing, ", "))
			}
			return nil
		},
	}
	addRuntimeFlags(cmd, &opts)
	return cmd
}

func renderSessions(out io.Writer, sessions []store.Session) {
	if len(sessions) == 0 {
		fmt.Fprintln(out, "no sessions yet — try `mosaic new <name>`")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATUS\tTOOL\tBRANCH\tWORKTREE\tLAST_ACTIVE")
	for _, s := range sessions {
		branch := s.Branch.String
		if branch == "" {
			branch = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Name,
			statusGlyph(s.Status)+s.Status,
			s.Tool,
			branch,
			s.WorktreePath,
			humanAge(s.LastActiveAt),
		)
	}
	_ = tw.Flush()
}

func statusGlyph(status string) string {
	switch store.SessionStatus(status) {
	case store.StatusRunning:
		return "● "
	case store.StatusWaiting:
		return "⚠ "
	case store.StatusError:
		return "✗ "
	case store.StatusStopped:
		return "■ "
	default:
		return "○ "
	}
}

func humanAge(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
