Set Columns 70
Set Rows 24
Set LineHeight 0.82
Set Theme "Dracula"
Set WaitTimeout 2m
Source assets/base.tape

Hide
Type@0 'cd "$DEMO_DIR/repos" && clear && ../bin/clone -O charmbracelet --vcs=git --exclude=vhs --height=0.95'
Enter
Wait+Screen /Fetch(ing|ed) +owner=/
Sleep 80ms
Show
Wait+Screen "total="
Wait /^0$/
