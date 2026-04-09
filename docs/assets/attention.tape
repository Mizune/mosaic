# "Which agent needs me right now?" — the mosaic killer
# feature. Shows 3 running sessions, then attach to one and
# trigger an attention via OSC 777 from the pane itself.
#
#   vhs docs/assets/attention.tape

Output docs/assets/attention.gif

Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1400
Set Height 800
Set Padding 20
Set Framerate 24

Hide
Type "tmux -L mosaic kill-server 2>/dev/null; true"
Enter
Type "export MOSAIC_HOME=/tmp/mosaic-demo-attention"
Enter
Type "rm -rf $MOSAIC_HOME"
Enter
Type "mkdir -p $MOSAIC_HOME"
Enter
Type "./bin/mosaic doctor --home $MOSAIC_HOME"
Enter
Sleep 1s
Type "clear"
Enter
Show

Type "./bin/mosaic --home $MOSAIC_HOME"
Enter
Sleep 3s

# create three sessions rapidly
Type "n"
Sleep 400ms
Type "build"
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 1500ms

Type "n"
Sleep 400ms
Type "test"
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 1500ms

Type "n"
Sleep 400ms
Type "lint"
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 300ms
Enter
Sleep 1500ms

# attach to the middle session and echo an OSC 777 so
# mosaic's capture scanner picks it up on return.
Down
Sleep 200ms
Down
Sleep 200ms
Enter
Sleep 2s

Type "printf 'notify: need input\n'"
Enter
Sleep 800ms

# Ctrl+Q to detach back to mosaic (mosaic-managed tmux.conf
# binds this to detach-client).
Ctrl+Q
Sleep 2s

# Jump to the next attention with N (capital)
Type "N"
Sleep 1500ms

# Dismiss it with D
Type "D"
Sleep 1500ms

# Quit
Type "q"
Sleep 1s
