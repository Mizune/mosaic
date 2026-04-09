# Diff View demo: open the view, filter to modified only,
# drill into a file, stage a hunk, commit inline.
#
#   vhs docs/assets/diff-view.tape
#
# Uses docs/assets/seed-diff-repo.sh to stand up a dirty git
# repo under /tmp/mosaic-diff-repo so the tape does not have
# to nest heredocs inside vhs Type commands (which breaks the
# vhs parser).

Output docs/assets/diff-view.gif

Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1400
Set Height 800
Set Padding 20
Set Framerate 24

Hide
Type "tmux -L mosaic kill-server 2>/dev/null; true"
Enter
Type "export MOSAIC_HOME=/tmp/mosaic-demo-diff"
Enter
Type "rm -rf $MOSAIC_HOME"
Enter
Type "mkdir -p $MOSAIC_HOME"
Enter
Type "bash docs/assets/seed-diff-repo.sh"
Enter
Sleep 500ms
Type "./bin/mosaic doctor --home $MOSAIC_HOME"
Enter
Sleep 500ms
Type "./bin/mosaic --home $MOSAIC_HOME new diff-demo --path /tmp/mosaic-diff-repo"
Enter
Sleep 500ms
Type "clear"
Enter
Show

Type "./bin/mosaic --home $MOSAIC_HOME"
Enter
Sleep 3s

# open the Diff View
Type "d"
Sleep 2s

# filter to modified only, then back to all
Type "M"
Sleep 1s
Type "a"
Sleep 1s

# move to the second file and drill in
Down
Sleep 500ms
Type "l"
Sleep 2s

# scroll the content
Type "J"
Sleep 800ms
Type "K"
Sleep 500ms

# back to browse
Type "h"
Sleep 500ms

# stage the current file
Type "s"
Sleep 800ms

# commit
Type "c"
Sleep 500ms
Type "show off mosaic diff view"
Sleep 800ms
Enter
Sleep 1500ms

# close diff view, quit
Type "q"
Sleep 500ms
Type "q"
Sleep 500ms
