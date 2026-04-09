# Cycles through every built-in theme from the Settings screen
# so the viewer can see the palette swap in real time.
#
#   vhs docs/assets/theme-switch.tape

Output docs/assets/theme-switch.gif

Set Theme "Catppuccin Mocha"
Set FontSize 14
Set Width 1400
Set Height 800
Set Padding 20
Set Framerate 24

Hide
Type "tmux -L mosaic kill-server 2>/dev/null; true"
Enter
Type "export MOSAIC_HOME=/tmp/mosaic-demo-theme"
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
Sleep 2s

# Seed a couple of sessions so the preview pane has content
# to repaint on every theme switch.
Type "n"
Sleep 300ms
Type "feat-auth"
Enter
Sleep 200ms
Enter
Sleep 200ms
Enter
Sleep 200ms
Enter
Sleep 1500ms

Type "n"
Sleep 300ms
Type "fix-login"
Enter
Sleep 200ms
Enter
Sleep 200ms
Enter
Sleep 200ms
Enter
Sleep 1500ms

# Open Settings
Type ","
Sleep 1s

# Arrow down to the theme field (default_tool → log_level → theme)
Down
Sleep 400ms
Down
Sleep 400ms

# Cycle through themes forward
Enter
Sleep 1200ms
Enter
Sleep 1200ms
Enter
Sleep 1200ms
Enter
Sleep 1200ms
Enter
Sleep 1200ms

# Save with Ctrl+S and exit
Ctrl+S
Sleep 800ms
Type "q"
Sleep 600ms

# Quit mosaic
Type "q"
Sleep 800ms
