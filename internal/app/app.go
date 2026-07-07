package app

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/KaraBala10/chatbang-pro/internal/chaturl"
	"github.com/KaraBala10/chatbang-pro/internal/cli"
	"github.com/KaraBala10/chatbang-pro/internal/config"
	"github.com/KaraBala10/chatbang-pro/internal/help"
	"github.com/KaraBala10/chatbang-pro/internal/prompt"
	"github.com/KaraBala10/chatbang-pro/internal/server"
	"github.com/KaraBala10/chatbang-pro/internal/session"
)

// Run is the application entry point.
func Run(version string, args []string) {
	usr, err := user.Current()
	if err != nil {
		fmt.Println("Error fetching user info:", err)
		return
	}

	paths := config.PathsForHome(usr.HomeDir)

	opts := cli.Parse(args, true)
	if opts.WantHelp {
		help.Print(paths.File)
		return
	}

	if err = os.MkdirAll(paths.Dir, 0o755); err != nil {
		fmt.Println("Error creating config directory:", err)
		return
	}

	configFile, err := os.OpenFile(paths.File, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		fmt.Println("Error opening config file:", err)
		return
	}
	defer configFile.Close()

	if _, err = configFile.Seek(0, io.SeekStart); err != nil {
		fmt.Println("Error reading config file:", err)
		return
	}
	defaultBrowser, headless := config.Parse(configFile)

	if defaultBrowser == "" {
		detectedBrowser, err := config.DetectBrowser()
		if err != nil {
			fmt.Println("No Chromium-based browser found in /bin, /usr/bin, or config.")
			fmt.Println("Please install a Chromium-based browser or edit the config at", paths.File)
			return
		}

		defaultBrowser = detectedBrowser
		defaultConfig := "browser=" + defaultBrowser + "\nheadless=" + strconv.FormatBool(headless) + "\n"

		if _, err = configFile.Seek(0, io.SeekStart); err != nil {
			fmt.Println("Error writing default config:", err)
			return
		}
		if _, err = io.WriteString(configFile, defaultConfig); err != nil {
			fmt.Println("Error writing default config:", err)
			return
		}
	}

	opts = cli.Parse(args, headless)
	if opts.WantConfig {
		session.LoginProfile(defaultBrowser, paths.Profile)
		return
	}

	if opts.KillBrowser {
		if err := session.KillBackgroundBrowser(); err != nil {
			log.Fatal(err)
		}
		return
	}

	if opts.ListSessions {
		items, err := session.ListSavedSessions(paths.Sessions)
		if err != nil {
			log.Fatal(err)
		}
		if len(items) == 0 {
			fmt.Println("No saved sessions.")
			return
		}
		for _, item := range items {
			fmt.Printf("%s\t%s\t%s\t%s\n", item.ID, item.UpdatedAt, item.Title, item.URL)
		}
		return
	}

	headless = opts.Headless
	var chatTarget string
	if opts.ServerMode {
		if opts.Resume != "" {
			log.Fatal("--resume is not supported with --server")
		}
		chatTarget, err = chaturl.Resolve(true, opts.CustomGPT)
		if err != nil {
			log.Fatal(err)
		}
	} else if opts.Resume != "" {
		item, err := session.ResolveSavedSession(paths.Sessions, opts.Resume)
		if err != nil {
			log.Fatal(err)
		}
		chatTarget = item.URL
		fmt.Fprintf(os.Stderr, "Resuming session %s: %s\n", item.ID, item.Title)
	} else {
		chatTarget, err = chaturl.Resolve(opts.TemporaryChat, opts.CustomGPT)
		if err != nil {
			log.Fatal(err)
		}
	}
	if opts.CustomGPT != "" {
		fmt.Fprintf(os.Stderr, "Custom GPT: %s\n", chatTarget)
	}
	if chaturl.IsTemporary(chatTarget) {
		fmt.Fprintln(os.Stderr, "Temporary chat mode — conversations are not saved to history.")
	}

	fmt.Fprintf(os.Stderr, "chatbang-pro %s\n", version)
	if opts.KeepBrowser {
		fmt.Fprintln(os.Stderr, "Opening ChatGPT with keep-browser mode…")
	} else {
		fmt.Fprintln(os.Stderr, "Starting browser and opening ChatGPT…")
	}
	sess, err := session.New(defaultBrowser, paths.Profile, headless, chatTarget, opts.KeepBrowser, paths.Sessions, chaturl.IsTemporary(chatTarget))
	if err != nil {
		log.Fatal(err)
	}
	defer sess.Close()
	if opts.ServerMode {
		mux := http.NewServeMux()
		server.NewHandler(sess).Register(mux)
		listenAddr := cli.ListenAddr(opts)
		fmt.Fprintf(os.Stderr, "OpenAI-compatible server listening on http://%s/v1/chat/completions\n", listenAddr)
		if err := http.ListenAndServe(listenAddr, mux); err != nil {
			log.Print(err)
		}
		return
	}

	if opts.MessageFlag && strings.TrimSpace(opts.Message) == "" {
		log.Fatal("--message requires a value")
	}
	if msg := strings.TrimSpace(opts.Message); msg != "" {
		sess.RunTurn(msg)
		return
	}

	fmt.Fprintln(os.Stderr, "Ready — start chatting below.")
	prompt.Loop(cli.IsExitCommand, sess.RunTurn)
	fmt.Fprintln(os.Stderr, "[debug] Run: prompt.Loop returned, defer sess.Close will run")
}
