package leetcode

import (
	"os"
	"strings"
	"testing"
)

func TestUnescapeContent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "converte quebras escapadas",
			in:   `a\nb`,
			want: "a\nb",
		},
		{
			name: "não mexe em conteúdo com quebras reais",
			in:   "a\nb com \\n dentro",
			want: "a\nb com \\n dentro",
		},
		{
			name: "preserva escapes desconhecidos",
			in:   `re.compile("\d+")\nx`,
			want: "re.compile(\"\\d+\")\nx",
		},
		{
			name: "barra dupla vira barra simples",
			in:   `path = "C:\\tmp"\nend`,
			want: "path = \"C:\\tmp\"\nend",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unescapeContent(tt.in); got != tt.want {
				t.Errorf("unescapeContent(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestExtractCodeRealArticle runs the extractor over a real (escaped) solution
// post captured from the API.
func TestExtractCodeRealArticle(t *testing.T) {
	data, err := os.ReadFile("testdata/escaped_article.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := ExtractCode(string(data), "python3", "twoSum")
	if len(blocks) == 0 {
		t.Fatal("nenhum bloco extraído do artigo real")
	}

	first := blocks[0]
	for _, want := range []string{"class Solution:", "def twoSum", "seen = {}"} {
		if !strings.Contains(first, want) {
			t.Errorf("primeiro bloco não contém %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, `\n`) {
		t.Errorf("bloco ainda tem \\n literal:\n%s", first)
	}
	// O post é um megapost e também traz threeSum; esses blocos não devem
	// entrar porque não contêm o entry point pedido.
	for i, b := range blocks {
		if !strings.Contains(b, "twoSum") {
			t.Errorf("bloco %d não contém o entry point:\n%s", i, b)
		}
	}
}
