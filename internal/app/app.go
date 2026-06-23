package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"strconv"
	"strings"

	"github.com/KaraBala10/chatbang-pro/internal/chaturl"
	"github.com/KaraBala10/chatbang-pro/internal/cli"
	"github.com/KaraBala10/chatbang-pro/internal/config"
	"github.com/KaraBala10/chatbang-pro/internal/help"
	"github.com/KaraBala10/chatbang-pro/internal/prompt"
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

	headless = opts.Headless
	chatTarget, err := chaturl.Resolve(opts.TemporaryChat, opts.CustomGPT)
	if err != nil {
		log.Fatal(err)
	}
	if opts.CustomGPT != "" {
		fmt.Fprintf(os.Stderr, "Custom GPT: %s\n", chatTarget)
	}
	if chaturl.IsTemporary(chatTarget) {
		fmt.Fprintln(os.Stderr, "Temporary chat mode — conversations are not saved to history.")
	}

	fmt.Fprintf(os.Stderr, "chatbang-pro %s\n", version)
	fmt.Fprintln(os.Stderr, "Starting browser and opening ChatGPT…")
	sess, err := session.New(defaultBrowser, paths.Profile, headless, chatTarget)
	if err != nil {
		log.Fatal(err)
	}
	defer sess.Close()

	if opts.MessageFlag && strings.TrimSpace(opts.Message) == "" {
		log.Fatal("--message requires a value")
	}
	if msg := strings.TrimSpace(opts.Message); msg != "" {
		sess.RunTurn(msg)
		return
	}

	fmt.Fprintln(os.Stderr, "Ready — start chatting below.")
	prompt.Loop(cli.IsExitCommand, sess.RunTurn)
}
