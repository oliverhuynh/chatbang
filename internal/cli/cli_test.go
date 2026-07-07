package cli

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		args []string
		base bool
		want Options
	}{
		{
			name: "defaults",
			args: []string{"chatbang-pro"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999"},
		},
		{
			name: "help",
			args: []string{"chatbang-pro", "--help"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999", WantHelp: true},
		},
		{
			name: "config",
			args: []string{"chatbang-pro", "--config"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999", WantConfig: true},
		},
		{
			name: "no headless overrides config default",
			args: []string{"chatbang-pro", "--no-headless"},
			base: true,
			want: Options{Headless: false, ListenHost: "127.0.0.1", Port: "19999"},
		},
		{
			name: "headless from config base false",
			args: []string{"chatbang-pro", "--headless"},
			base: false,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999"},
		},
		{
			name: "temporary chat",
			args: []string{"chatbang-pro", "--temp"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999", TemporaryChat: true},
		},
		{
			name: "gpt flag",
			args: []string{"chatbang-pro", "-g", "g-abc123"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999", CustomGPT: "g-abc123"},
		},
		{
			name: "gpt equals form",
			args: []string{"chatbang-pro", "--gpt=g-abc123"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999", CustomGPT: "g-abc123"},
		},
		{
			name: "message flag",
			args: []string{"chatbang-pro", "-m", "hello"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999", MessageFlag: true, Message: "hello"},
		},
		{
			name: "message equals form",
			args: []string{"chatbang-pro", "--message=hello"},
			base: true,
			want: Options{Headless: true, ListenHost: "127.0.0.1", Port: "19999", MessageFlag: true, Message: "hello"},
		},
		{
			name: "combined flags",
			args: []string{"chatbang-pro", "--temp", "-g", "g-abc123", "-m", "hi"},
			base: true,
			want: Options{
				Headless:      true,
				ListenHost:    "127.0.0.1",
				Port:          "19999",
				TemporaryChat: true,
				CustomGPT:     "g-abc123",
				MessageFlag:   true,
				Message:       "hi",
			},
		},
		{
			name: "server listen host",
			args: []string{"chatbang-pro", "--server", "--listen", "0.0.0.0"},
			base: true,
			want: Options{Headless: true, ServerMode: true, ListenHost: "0.0.0.0", Port: "19999"},
		},
		{
			name: "server port flag",
			args: []string{"chatbang-pro", "--server", "--port", "20000"},
			base: true,
			want: Options{Headless: true, ServerMode: true, ListenHost: "127.0.0.1", Port: "20000"},
		},
		{
			name: "listen host plus port",
			args: []string{"chatbang-pro", "--server", "--listen", "0.0.0.0", "--port", "20000"},
			base: true,
			want: Options{Headless: true, ServerMode: true, ListenHost: "0.0.0.0", Port: "20000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.args, tt.base)
			if got != tt.want {
				t.Fatalf("Parse() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestListenAddr(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "defaults",
			opts: Options{ListenHost: "127.0.0.1", Port: "19999"},
			want: "127.0.0.1:19999",
		},
		{
			name: "custom host",
			opts: Options{ListenHost: "0.0.0.0", Port: "19999"},
			want: "0.0.0.0:19999",
		},
		{
			name: "custom port",
			opts: Options{ListenHost: "127.0.0.1", Port: "20000"},
			want: "127.0.0.1:20000",
		},
		{
			name: "bracketed ipv6 host",
			opts: Options{ListenHost: "[::1]", Port: "19999"},
			want: "[::1]:19999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ListenAddr(tt.opts); got != tt.want {
				t.Fatalf("ListenAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsExitCommand(t *testing.T) {
	for _, cmd := range []string{"exit", "EXIT", " quit ", "q", ":q", "/exit"} {
		if !IsExitCommand(cmd) {
			t.Fatalf("%q should be an exit command", cmd)
		}
	}
	if IsExitCommand("hello") {
		t.Fatal("hello should not be an exit command")
	}
}
