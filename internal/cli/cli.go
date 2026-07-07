package cli

import (
	"net"
	"strings"
)

// Options holds parsed command-line flags.
type Options struct {
	WantConfig    bool
	WantHelp      bool
	Headless      bool
	KeepBrowser   bool
	KillBrowser   bool
	ServerMode    bool
	ListenHost    string
	Port          string
	TemporaryChat bool
	CustomGPT     string
	Message       string
	MessageFlag   bool
	ListSessions  bool
	Resume        string
}

func flagValue(args []string, i int) (string, int, bool) {
	if i+1 >= len(args) {
		return "", i, false
	}
	return args[i+1], i + 1, true
}

func setCustomGPT(opts *Options, value string) {
	opts.CustomGPT = strings.TrimSpace(value)
}

// Parse applies flags and modes. --config takes precedence over --help.
func Parse(args []string, headless bool) Options {
	opts := Options{Headless: headless, ListenHost: "127.0.0.1", Port: "19999"}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--config":
			opts.WantConfig = true
		case "--help", "-h":
			opts.WantHelp = true
		case "--headless":
			opts.Headless = true
		case "--no-headless":
			opts.Headless = false
		case "--temporary-chat", "--temp":
			opts.TemporaryChat = true
		case "--keep-browser":
			opts.KeepBrowser = true
		case "--kill-browser":
			opts.KillBrowser = true
		case "--server":
			opts.ServerMode = true
		case "--listen":
			value, next, ok := flagValue(args, i)
			if !ok {
				continue
			}
			opts.ListenHost = strings.TrimSpace(value)
			i = next
		case "--port":
			value, next, ok := flagValue(args, i)
			if !ok {
				continue
			}
			opts.Port = strings.TrimSpace(value)
			i = next
		case "--sessions":
			opts.ListSessions = true
		case "--resume":
			value, next, ok := flagValue(args, i)
			if !ok {
				continue
			}
			opts.Resume = strings.TrimSpace(value)
			i = next
		case "--gpt", "--custom-gpt", "-g":
			value, next, ok := flagValue(args, i)
			if !ok {
				continue
			}
			setCustomGPT(&opts, value)
			i = next
		case "--message", "-m":
			opts.MessageFlag = true
			value, next, ok := flagValue(args, i)
			if ok {
				opts.Message = value
				i = next
			}
		default:
			if value, ok := strings.CutPrefix(arg, "--gpt="); ok {
				setCustomGPT(&opts, value)
			} else if value, ok := strings.CutPrefix(arg, "--custom-gpt="); ok {
				setCustomGPT(&opts, value)
			} else if value, ok := strings.CutPrefix(arg, "--listen="); ok {
				opts.ListenHost = strings.TrimSpace(value)
			} else if value, ok := strings.CutPrefix(arg, "--port="); ok {
				opts.Port = strings.TrimSpace(value)
			} else if value, ok := strings.CutPrefix(arg, "--resume="); ok {
				opts.Resume = strings.TrimSpace(value)
			} else if value, ok := strings.CutPrefix(arg, "--message="); ok {
				opts.MessageFlag = true
				opts.Message = value
			}
		}
	}
	return opts
}

func ListenAddr(opts Options) string {
	host := strings.TrimSpace(opts.ListenHost)
	if host == "" {
		host = "127.0.0.1"
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	port := strings.TrimSpace(opts.Port)
	if port == "" {
		port = "19999"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
			host = parsedHost
			if opts.Port == "" && parsedPort != "" {
				port = parsedPort
			}
		}
	}
	return net.JoinHostPort(host, port)
}

// IsExitCommand reports whether the user typed a quit command.
func IsExitCommand(prompt string) bool {
	switch strings.ToLower(strings.TrimSpace(prompt)) {
	case "exit", "quit", "q", ":q", "/exit", "/quit":
		return true
	default:
		return false
	}
}
