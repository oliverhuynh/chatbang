package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chromedp/chromedp"
)

var browserSearchDirs = []string{"/bin/", "/usr/bin/"}

var browsers = []string{
	"chromium",
	"chromium-browser",
	"google-chrome",
	"google-chrome-stable",
	"microsoft-edge",
	"microsoft-edge-stable",
	"brave-browser",
	"vivaldi",
	"opera",
	"msedge",
	"ungoogled-chromium",
}

// Paths holds config and profile locations under the user home directory.
type Paths struct {
	Dir     string
	File    string
	Profile string
}

// PathsForHome returns standard chatbang config paths for a home directory.
func PathsForHome(homeDir string) Paths {
	dir := filepath.Join(homeDir, ".config", "chatbang")
	return Paths{
		Dir:     dir,
		File:    filepath.Join(dir, "chatbang"),
		Profile: filepath.Join(dir, "profile_data"),
	}
}

func AllocatorOptions(browserPath, profileDir string, headless bool) []chromedp.ExecAllocatorOption {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(browserPath),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("disable-extensions", false),
		chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		chromedp.Flag("disable-default-apps", false),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("profile-directory", "Default"),
	)
	opts = append(opts, chromedp.Flag("headless", headless))
	return opts
}

func DetectBrowser() (string, error) {
	for _, dir := range browserSearchDirs {
		for _, name := range browsers {
			path := dir + name
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("no Chromium-based browser found in /bin or /usr/bin")
}

func parseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// Parse reads browser and headless settings from a config file reader.
func Parse(r io.Reader) (browser string, headless bool) {
	headless = true

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "browser":
			browser = value
		case "headless":
			if parsed, ok := parseBool(value); ok {
				headless = parsed
			}
		}
	}
	return browser, headless
}
