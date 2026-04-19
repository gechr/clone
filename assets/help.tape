Set Shell bash
Set FontSize 22
Set Padding 15
Set Margin 10
Set MarginFill "#674EFF"
Set BorderRadius 10
Set Theme { "background": "#000000", "foreground": "#edfffe", "cursor": "#ff5200", "black": "#0a0a0a", "red": "#ff5f82", "green": "#b8ea6e", "yellow": "#ffcf3f", "blue": "#6fdcf0", "magenta": "#b49bff", "cyan": "#2ee5d4", "white": "#e3e3e3", "brightBlack": "#c1c1c1", "brightRed": "#ff2a55", "brightGreen": "#bbf262", "brightYellow": "#ffcf3f", "brightBlue": "#5fd1ff", "brightMagenta": "#c066ff", "brightCyan": "#00fff0", "brightWhite": "#ffffff" }
Set Width 1420
Set Height 1800

Env COLORTERM "truecolor"
Env PS1 ""

Hide
Type "tput civis && go build -o /tmp/clone-demo ."
Enter
Type "tput civis && clear && /tmp/clone-demo -h && sleep 1"
Enter
Sleep 2s
Show
Sleep 1s
Screenshot assets/help.png
