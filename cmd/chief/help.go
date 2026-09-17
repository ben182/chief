package main

import "fmt"

func printHelp() {
	fmt.Println(`Chief - Autonomous PRD Agent

Usage:
  chief [options] [<name>|<path/to/prd.md>]
  chief <command> [arguments]

Commands:
  start [name]              Launch the TUI and begin the loop immediately
                            (add --headless to run without a TUI, e.g. over SSH)
  new [name] [context]      Create a new PRD interactively (prompts for the Claude model unless --model is set)
  edit [name] [options]     Edit an existing PRD interactively (prompts for the Claude model unless --model is set)
  followup [name]           Convert a PRD's follow-up inbox (todos.md) into new user stories
                            (name defaults to the PRD of the current chief/<name> branch)
  status [name]             Show progress for a PRD (default: default)
  list                      List all PRDs with progress
  worktree <cmd> <prd>      Run a PRD worktree's setup or teardown command by hand
  help                      Show this help message

Global Options:
  --agent <provider>        Agent CLI to use: claude (default), codex, opencode, cursor, or gemini
  --agent-path <path>       Custom path to agent CLI binary
  --model <model>           Model passed to the agent CLI via --model (Claude only)
  --max-iterations N, -n N  Set maximum iterations (default: dynamic)
  --no-retry                Disable auto-retry on agent crashes
  --verbose                 Show raw agent output in log
  --headless                Run without the TUI, logging to stdout. For a machine
                            nobody is sitting at: survives a dropped SSH session
                            under nohup/systemd, and exits 0 only when every story
                            is resolved. Never asks a question
  --worktree                With --headless, run in the PRD's own git worktree
                            (branch chief/<name>) instead of the current checkout
  --help, -h                Show this help message
  --version, -v             Show version number

Positional Arguments:
  <name>                    PRD name (loads .chief/prds/<name>/prd.md)
  <path/to/prd.md>        Direct path to a prd.md file

Examples:
  chief                     Launch TUI with default PRD (.chief/prds/default/)
  chief auth                Launch TUI with named PRD (.chief/prds/auth/)
  chief start               Launch default PRD and start the loop immediately
  chief start auth          Launch auth PRD and start the loop immediately
  chief ./my-prd.md       Launch TUI with specific PRD file
  chief -n 20               Launch with 20 max iterations
  chief --max-iterations=5 auth
                            Launch auth PRD with 5 max iterations
  chief --verbose           Launch with raw agent output visible
  chief start auth --headless
                            Run the auth PRD to completion with no TUI
  chief start auth --headless --worktree > run.log 2>&1
                            The same, in its own worktree, logging to a file
  chief --agent codex       Use Codex CLI instead of Claude
  chief --agent cursor      Use Cursor CLI as agent
  chief --model my-local-model
                            Pass --model to Claude (e.g. local models via LM Studio)
  chief new                 Create PRD in .chief/prds/default/
  chief new auth            Create PRD in .chief/prds/auth/
  chief new auth "JWT authentication for REST API"
                            Create PRD with context hint
  chief edit                Edit PRD in .chief/prds/default/
  chief edit auth           Edit PRD in .chief/prds/auth/
  chief followup auth       Turn .chief/prds/auth/todos.md items into new stories
  chief status              Show progress for default PRD
  chief status auth         Show progress for auth PRD
  chief list                List all PRDs with progress
  chief worktree setup auth Re-run worktree.setup in the auth PRD's worktree
  chief worktree teardown auth
                            Run worktree.teardown without removing the worktree
  chief --version           Show version number`)
}

func printWiggum() {
	// ANSI color codes
	blue := "\033[34m"
	yellow := "\033[33m"
	reset := "\033[0m"

	art := blue + `
                                                                 -=
                                      +%#-   :=#%#**%-
                                     ##+**************#%*-::::=*-
                                   :##***********************+***#
                                 :@#********%#%#******************#*
                                 :##*****%+-:::-%%%%%##************#:
                                   :#%###%%-:::+#*******##%%%*******#%*:
                                      -+%**#%%@@%%%%%%%%%#****#%##*##%%=
                                      -@@%%%%%%%%%%%%%%@*#%%#*##:::
                                    +%%%%%%%%%%%%%%@#+--=#--=#@+:
                                   -@@@@@%@@@@#%#=-=**--+*-----=#:
` + yellow + `                                       :*     *-   - :#-:*=-----=#:
                                       %::%@- *:  *@# +::=*--#=:-%:
                                       #- =+**##-    =*:::#*#-++:*:
                                        #+:-::+--%***-::::::::-*##
                                      :+#:+=:-==-*:::::::::::::::-%
                                     *=::::::::::::::-=*##*:::::::-+
                                     *-::::::::-=+**+-+%%%%+:::::--+
                                      :*%##**==++%%%######%:::::--%-
                                        :-=#--%####%%%%@@+:::::--%=
` + blue + `                     -#%%%%#-` + yellow + `          *:::+%%##%%#%%*:::::::-*#%-
                   :##++++=+++%:` + yellow + `        :@%*:::::::::::::::-=##*%%*%=
                  :%++++@%#+=++#` + yellow + `         %%%=--:::::---=+%%****%##@%#%%*:
                -%=-:-%%%*=+++##` + yellow + `      :*@%***@%%%###*********%%#%********%-
               *#+==**%++++++#*-` + yellow + `   :*%@*+*%*%%%%@*********%%**##****%=--#%*#
             *%#%-:+*++++*%#=#-` + yellow + `  :%#%#*+***#@%%%@%#%%%@%#*****%****%::::::##%-
            :*::::*-%@%@#=*%-` + yellow + `  :%*#%+*******%%%@#*************%****%-::::::**%=
             +==%*+-----+%` + yellow + `    %#*%#********#@%%@********%*%***#%**+*%-:::::*#*%:
              *=::----##**%:` + yellow + `+%#*@**********@%%%%*+***%-::::::#*%#****%#:::-%***%-
               #-:+@#***+*@%` + yellow + `**#%**********%%%#%%*****%::::::-#**%***************%
               =%*****+%%+**` + yellow + `@#%***********@%#%%#******%:::::%****@*********+****##
` + blue + `                %*#%@#*+++**#%` + yellow + `************%%%%%#********###*******@**************%:
                =#**++***+**@` + yellow + `************%%%%#%%*******************%*************##
                 %*++******@#` + yellow + `************@%%#%%@*******************#@*************@:
                  #***+***%#*` + yellow + `************@%%%%%@#*******************#%*************+
                   +#***##%**` + yellow + `************@%%%%%%%********************%************%
                     :######**` + yellow + `*+**********%%%%%%%%*********************%************%
                       :+%@#**` + yellow + `*******+*****#%@@%#******+***************#@*****+*****%:
` + blue + `                         @*********************************************##*+**+*****#+
                        =%%%%%@@@%%#**************************##%%@@@%%%@**********##
                        =%%#%%%%%%%%%%%%%----====%%%%%%%%%%%%%%%%#%%#%%%%%******#%#*%
                        :@@%%#%%%%%%%%%%#::::::::*%%%%%%%%%%%%%%%%%%#%%%@@#%%%##***#%
                          %*##%%@@@@%%%%%::::::::#%%%%%%%@@@@@@%%####****##****#%#==#
                          :%*********************************************#%#*+=-----*-
                           :%************************************+********@:::::----=+
                             ##**********+******************+************##::-::=--#-%
                              =%******************+*+*********************%:=-*:++:#-%
                               *#*****************************************@*#:*:*=:*+=
                                %*********#%#**************************+*%   -#+%**=:
                                **************#%%%%###*******************#
                                =#***************%      #****************#
                                :@***+**********##      *****************#
                                 %**************#=      =#+******+*******#
                                 =#*************%:      :@***************#
                                 :#****+********#        #***************#
                                 :#**************        =#**************#
                                 :%************%-        :%*************##
                                  #***********##          %*************%=
                                -%@@@%######%@@+          =%#***#*#%@@%#@:
                              :%%%%%%%%%%%%%%%%#         +@%%%%%%%%%%%%%%*
                             +@%%%%%%%%%%%%%%%%+       :%%%%%%%%%%%%%%##@+
                             #%%%%%%%%%%%@%@%@*       :@%%%%%%%%%%%%@%%@*
` + reset + `
                         "Bake 'em away, toys!"
                               - Chief Wiggum
`
	fmt.Print(art)
}
