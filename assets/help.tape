Set Columns 97
Set Rows 71
Set Theme { "background": "#000000", "foreground": "#edfffe", "cursor": "#ff5200", "black": "#0a0a0a", "red": "#ff5f82", "green": "#b8ea6e", "yellow": "#ffcf3f", "blue": "#6fdcf0", "magenta": "#b49bff", "cyan": "#2ee5d4", "white": "#e3e3e3", "brightBlack": "#c1c1c1", "brightRed": "#ff2a55", "brightGreen": "#bbf262", "brightYellow": "#ffcf3f", "brightBlue": "#5fd1ff", "brightMagenta": "#c066ff", "brightCyan": "#00fff0", "brightWhite": "#ffffff" }
Source assets/base.tape

Type@0 'clear && "$DEMO_DIR/bin/clone" -h'
Enter
Wait+Screen "Print long help with examples"
Wait /^0$/
ScrollUp 1
Sleep 80ms
