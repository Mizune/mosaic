# 45-second high-level tour of mosaic. Shows the landing TUI,
# the session wizard, attention detection, the preview tab
# toggle, and the Diff View. Regenerate with:
#
#   vhs docs/assets/tour.tape
#
# The binary must be built beforehand (`make build`).

Output docs/assets/tour.gif

Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1400
Set Height 800
Set Padding 20
Set Framerate 24

# A sandbox so the tape never touches the real ~/.mosaic state.
Hide
Type "tmux -L mosaic kill-server 2>/dev/null; true"
Enter
Type "export MOSAIC_HOME=/tmp/mosaic-demo-tour"
Enter
Type "rm -rf $MOSAIC_HOME"
Enter
Type "mkdir -p $MOSAIC_HOME"
Enter
Type "clear"
Enter
Show

Type "./bin/mosaic doctor --home $MOSAIC_HOME"
Enter
Sleep 2s

Type "clear"
Enter
Type "./bin/mosaic --home $MOSAIC_HOME"
Enter
Sleep 3s

# open the wizard
Type "n"
Sleep 1s
Type "feat-demo"
Sleep 500ms
Enter
Sleep 1s
Enter
Sleep 500ms
Enter
Sleep 500ms
Enter
Sleep 2s

# move the cursor, show the preview pane following
Down
Sleep 300ms
Down
Sleep 300ms

# cycle the preview modes with Tab
Tab
Sleep 1s
Tab
Sleep 1s
Tab
Sleep 1s
Tab
Sleep 1s

# open the Diff View
Type "d"
Sleep 2s

# move through a few files
Down
Sleep 500ms
Down
Sleep 500ms

# close the Diff View
Type "q"
Sleep 1s

# open the Conductor panel
Type "c"
Sleep 2s
Type "q"
Sleep 500ms

# quit mosaic
Type "q"
Sleep 1s
