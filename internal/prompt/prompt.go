package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	prefix      = "> "
	placeholder = "Ask anything… (type exit to quit)"
)

func readRuneFromStdin() (rune, error) {
	buf := make([]byte, 0, utf8.UTFMax)
	for {
		var b [1]byte
		n, err := os.Stdin.Read(b[:])
		if n == 0 {
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		buf = append(buf, b[0])
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 && len(buf) < utf8.UTFMax {
			continue
		}
		return r, nil
	}
}

// ReadLine reads one interactive prompt line from stdin.
func ReadLine() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Print(prefix)
		if !scanner.Scan() {
			if scanErr := scanner.Err(); scanErr != nil {
				return "", scanErr
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}
	defer term.Restore(fd, oldState)

	fmt.Print(prefix)
	fmt.Print("\x1b[2m" + placeholder + "\x1b[0m")

	var line []rune
	placeholderVisible := true

	redraw := func() {
		fmt.Print("\r" + prefix + "\x1b[K")
		fmt.Print(string(line))
	}

	for {
		r, err := readRuneFromStdin()
		if err != nil {
			fmt.Println()
			return "", err
		}

		switch r {
		case '\r', '\n':
			fmt.Print("\r\n")
			return string(line), nil
		case 4: // Ctrl+D
			fmt.Print("\r\n")
			return "", io.EOF
		case 3: // Ctrl+C
			fmt.Print("\r\x1b[K")
			fmt.Println("Type exit or quit to leave.")
			line = line[:0]
			placeholderVisible = true
			fmt.Print(prefix)
			fmt.Print("\x1b[2m" + placeholder + "\x1b[0m")
			continue
		case 127, 8:
			if len(line) > 0 {
				line = line[:len(line)-1]
				if len(line) == 0 {
					placeholderVisible = true
					fmt.Print("\r" + prefix + "\x1b[K\x1b[2m" + placeholder + "\x1b[0m")
				} else {
					redraw()
				}
			}
		case unicode.ReplacementChar:
			continue
		default:
			if !unicode.IsPrint(r) {
				continue
			}
			if placeholderVisible {
				placeholderVisible = false
				fmt.Print("\r" + prefix + "\x1b[K")
			}
			line = append(line, r)
			fmt.Print(string(r))
		}
	}
}

// Loop reads prompts until EOF, empty lines are skipped, and isExit returns true.
func Loop(isExit func(string) bool, onPrompt func(string)) {
	for {
		line, err := ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			fmt.Fprintln(os.Stderr, "prompt:", err)
			return
		}

		prompt := strings.TrimSpace(line)
		if prompt == "" {
			continue
		}
		if isExit(prompt) {
			return
		}
		onPrompt(prompt)
	}
}
