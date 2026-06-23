package config

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	browser, headless := Parse(strings.NewReader(`# comment
browser=/usr/bin/chromium
headless=false
`))
	if browser != "/usr/bin/chromium" || headless != false {
		t.Fatalf("got browser=%q headless=%v", browser, headless)
	}

	_, headless = Parse(strings.NewReader(""))
	if headless != true {
		t.Fatal("default headless should be true")
	}
}

func TestPathsForHome(t *testing.T) {
	p := PathsForHome("/home/user")
	if p.File != "/home/user/.config/chatbang/chatbang" {
		t.Fatalf("unexpected config file path: %s", p.File)
	}
	if p.Profile != "/home/user/.config/chatbang/profile_data" {
		t.Fatalf("unexpected profile path: %s", p.Profile)
	}
}
