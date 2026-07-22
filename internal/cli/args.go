// Package cli parses chief's command-line arguments. It is deliberately free of
// os.Exit and stdout side effects so the parsing rules can be unit-tested; the
// main package turns the returned Options/errors into program behavior.
package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ben182/chief/internal/prd"
)

// Options holds the parsed command-line options for TUI mode.
type Options struct {
	PRDPath       string
	MaxIterations int // 0 signals dynamic calculation (remaining stories + 5)
	Verbose       bool
	NoRetry       bool
	Agent         string // --agent claude|codex|opencode|cursor|gemini
	AgentPath     string // --agent-path
	Model         string // --model
	AutoStart     bool   // chief start: begin the loop automatically
	ShowHelp      bool   // --help/-h was requested
	ShowVersion   bool   // --version/-v was requested
}

// AgentFlags extracts --agent, --agent-path and --model from args[startIdx:],
// returning the agent name, path, model and the remaining (non-agent) args. It
// errors on a flag missing its value rather than exiting, so callers control
// how the failure surfaces.
func AgentFlags(args []string, startIdx int) (agentName, agentPath, model string, remaining []string, err error) {
	for i := startIdx; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--model":
			if i+1 >= len(args) {
				return "", "", "", nil, fmt.Errorf("--model requires a value")
			}
			i++
			model = args[i]
		case strings.HasPrefix(arg, "--model="):
			model = strings.TrimPrefix(arg, "--model=")
		case arg == "--agent":
			if i+1 >= len(args) {
				return "", "", "", nil, fmt.Errorf("--agent requires a value (claude, codex, opencode, cursor or gemini)")
			}
			i++
			agentName = args[i]
		case strings.HasPrefix(arg, "--agent="):
			agentName = strings.TrimPrefix(arg, "--agent=")
		case arg == "--agent-path":
			if i+1 >= len(args) {
				return "", "", "", nil, fmt.Errorf("--agent-path requires a value")
			}
			i++
			agentPath = args[i]
		case strings.HasPrefix(arg, "--agent-path="):
			agentPath = strings.TrimPrefix(arg, "--agent-path=")
		default:
			remaining = append(remaining, arg)
		}
	}
	return agentName, agentPath, model, remaining, nil
}

// ParseArgs parses TUI-mode flags. args should be the program arguments without
// the program name (i.e. os.Args[1:]). --help/--version set the corresponding
// flag on Options and stop parsing; any other error is returned.
func ParseArgs(args []string) (*Options, error) {
	opts := &Options{}

	// Pre-extract agent flags so they don't interfere with positional parsing.
	agentName, agentPath, model, _, err := AgentFlags(args, 0)
	if err != nil {
		return nil, err
	}
	opts.Agent, opts.AgentPath, opts.Model = agentName, agentPath, model

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--help" || arg == "-h":
			opts.ShowHelp = true
			return opts, nil
		case arg == "--version" || arg == "-v":
			opts.ShowVersion = true
			return opts, nil
		case arg == "--verbose":
			opts.Verbose = true
		case arg == "--no-retry":
			opts.NoRetry = true
		case arg == "--agent" || arg == "--agent-path" || arg == "--model":
			i++ // skip value (already parsed by AgentFlags)
		case strings.HasPrefix(arg, "--agent=") || strings.HasPrefix(arg, "--agent-path=") || strings.HasPrefix(arg, "--model="):
			// already parsed by AgentFlags
		case arg == "--max-iterations" || arg == "-n":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s requires a value", arg)
			}
			i++
			n, err := parseMaxIterations(arg, args[i])
			if err != nil {
				return nil, err
			}
			opts.MaxIterations = n
		case strings.HasPrefix(arg, "--max-iterations="):
			n, err := parseMaxIterations("--max-iterations", strings.TrimPrefix(arg, "--max-iterations="))
			if err != nil {
				return nil, err
			}
			opts.MaxIterations = n
		case strings.HasPrefix(arg, "-n="):
			n, err := parseMaxIterations("-n", strings.TrimPrefix(arg, "-n="))
			if err != nil {
				return nil, err
			}
			opts.MaxIterations = n
		case strings.HasPrefix(arg, "-"):
			return nil, fmt.Errorf("unknown flag: %s", arg)
		default:
			// Positional argument: PRD name or path.
			opts.PRDPath = PRDPathFromArg(arg)
		}
	}

	return opts, nil
}

// parseMaxIterations validates a max-iterations value: a positive integer.
func parseMaxIterations(flag, val string) (int, error) {
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid value for %s: %s", flag, val)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s must be at least 1", flag)
	}
	return n, nil
}

// PRDPathFromArg turns a positional argument into a prd.md path. A value that
// looks like a path (ends in .md, .json or /) is used verbatim; anything else is
// treated as a PRD name and mapped to .chief/prds/<name>/prd.md.
func PRDPathFromArg(arg string) string {
	if strings.HasSuffix(arg, ".md") || strings.HasSuffix(arg, ".json") || strings.HasSuffix(arg, "/") {
		return arg
	}
	return prd.PRDPath("", arg)
}

// FindAvailablePRD returns the path to the first PRD found under baseDir's
// .chief/prds/, or "" if none exist. baseDir may be "" for the current directory.
func FindAvailablePRD(baseDir string) string {
	entries, err := os.ReadDir(prd.PrdsDir(baseDir))
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			prdPath := prd.PRDPath(baseDir, entry.Name())
			if _, err := os.Stat(prdPath); err == nil {
				return prdPath
			}
		}
	}
	return ""
}

// ListAvailablePRDs returns the names of all PRDs under baseDir's .chief/prds/.
// baseDir may be "" for the current directory.
func ListAvailablePRDs(baseDir string) []string {
	entries, err := os.ReadDir(prd.PrdsDir(baseDir))
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(prd.PRDPath(baseDir, entry.Name())); err == nil {
				names = append(names, entry.Name())
			}
		}
	}
	return names
}
