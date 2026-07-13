package simplify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFromString(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "pre code block with br line breaks",
			html: `<div><p>Hello</p><pre><code><span>import</span><span> </span><span>requests</span><br><span>print</span><span>(</span><span>"ok"</span><span>)</span></code></pre><p>Done</p></div>`,
			want: "Hello\n\n```\nimport requests\nprint(\"ok\")\n```\n\nDone",
		},
		{
			name: "pre code inline spans no br",
			html: `<div><p>Test</p><pre><code><span>one</span><span> </span><span>two</span></code></pre><p>End</p></div>`,
			want: "Test\n\n```\none two\n```\n\nEnd",
		},
		{
			name: "inline code",
			html: `<p>Use <code>foo()</code> to call it.</p>`,
			want: "Use foo() to call it.",
		},
		{
			name: "nested pre code structure",
			html: `<div><p>Code:</p><pre><div><div><pre class="cm-content"><code><span>x</span><span> </span><span>=</span><span> </span><span>1</span><br><span>y</span><span> </span><span>=</span><span> </span><span>2</span></code></pre></div></div></pre><p>Done</p></div>`,
			want: "Code:\n\n```\n\nx = 1\ny = 2\n\n```\n\nDone",
		},
		{
			name: "unordered list",
			html: `<ul><li>A</li><li>B</li></ul>`,
			want: "- A\n- B",
		},
		{
			name: "ordered list",
			html: `<ol><li>First</li><li>Second</li></ol>`,
			want: "1. First\n2. Second",
		},
		{
			name: "table to markdown",
			html: `<table data-start="0" data-end="257" data-is-last-node="" data-is-only-node="" class="w-fit min-w-(--thread-content-width)"><thead data-start="0" data-end="19"><tr data-start="0" data-end="19"><th data-start="0" data-end="8" data-col-size="sm" class="last:pe-10">Field</th><th data-start="8" data-end="19" data-col-size="lg" class="last:pe-10">Details</th></tr></thead><tbody data-start="30" data-end="257" data-is-last-node=""><tr data-start="30" data-end="47"><td data-start="30" data-end="37" data-col-size="sm">Name</td><td data-col-size="lg" data-start="37" data-end="47">Oliver</td></tr><tr data-start="48" data-end="81"><td data-start="48" data-end="57" data-col-size="sm">Career</td><td data-col-size="lg" data-start="57" data-end="81">Full-stack developer</td></tr><tr data-start="82" data-end="132"><td data-start="82" data-end="94" data-col-size="sm">Expertise</td><td data-col-size="lg" data-start="94" data-end="132">Front-end and back-end development</td></tr><tr data-start="133" data-end="257" data-is-last-node=""><td data-start="133" data-end="151" data-col-size="sm">Profile Summary</td><td data-col-size="lg" data-start="151" data-end="257" data-is-last-node="">Oliver is a full-stack developer who works across both client-side interfaces and server-side systems.</td></tr></tbody></table>`,
			want: "| Field | Details |\n| --- | --- |\n| Name | Oliver |\n| Career | Full-stack developer |\n| Expertise | Front-end and back-end development |\n| Profile Summary | Oliver is a full-stack developer who works across both client-side interfaces and server-side systems. |",
		},
		{
			name: "buttons and svg stripped",
			html: `<p>Text</p><button>Click</button><svg><path d="..."/></svg><p>More</p>`,
			want: "Text\n\nMore",
		},
		{
			name: "paragraphs with newlines",
			html: `<p>Para one</p><p>Para two</p>`,
			want: "Para one\n\nPara two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromString(tt.html)
			if got != tt.want {
				t.Fatalf("FromString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test-dom.html")
	if err := os.WriteFile(path, []byte(`<p>Saved reply</p>`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := FromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Saved reply" {
		t.Fatalf("FromFile() = %q, want %q", result, "Saved reply")
	}
}
