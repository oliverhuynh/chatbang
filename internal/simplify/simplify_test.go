package simplify

import "testing"

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
	result, err := FromFile("/tmp/test-dom.html")
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal("FromFile returned empty result")
	}
}
