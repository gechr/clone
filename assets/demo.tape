Output assets/demo.gif

Set Shell bash
Set FontSize 22
Set Framerate 60
Set Padding 15
Set Margin 10
Set MarginFill "#674EFF"
Set BorderRadius 10
Set PlaybackSpeed 0.5
Set TypingSpeed 10ms
Set Theme "Dracula"
Set Width 1250
Set Height 750

Env COLORTERM "truecolor"
Env PS1 "\[\e[38;5;208m\]$\[\e[0m\] "

Hide
Type "tput civis && go build -o /tmp/clone-demo ."
Enter
Wait /^\$$/
Type "tput civis && clear && /tmp/clone-demo -O charmbracelet --temp --quick --vcs=git --exclude=vhs --height=0.95 && sleep 5 && clear"
Enter
Sleep 500ms
Show
Wait /^\$$/
