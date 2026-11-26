Set Shell bash
Set FontSize 25
Set Framerate 50
Set Padding 10
Set Margin 10
Set MarginFill "#674EFF"
Set BorderRadius 10

# Wait for a successful exit without showing a prompt in the recording.
Env PS1 '\[\e[?25l\e[8m\]$?\[\e[0m\]'
