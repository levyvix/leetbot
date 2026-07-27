package leetcode

import (
	"strings"
	"testing"
)

func TestExtractCode(t *testing.T) {
	tests := []struct {
		name       string
		markdown   string
		lang       string
		entryPoint string
		want       []string
	}{
		{
			name:       "fence simples",
			markdown:   "Explicação\n\n```python3\nclass Solution:\n    def twoSum(self, nums, target):\n        return []\n```\n",
			lang:       "python3",
			entryPoint: "twoSum",
			want:       []string{"class Solution:\n    def twoSum(self, nums, target):\n        return []"},
		},
		{
			name:       "alias py e info entre colchetes",
			markdown:   "```[Python3]\nclass Solution:\n    def twoSum(self): pass\n```",
			lang:       "python3",
			entryPoint: "twoSum",
			want:       []string{"class Solution:\n    def twoSum(self): pass"},
		},
		{
			name:       "descarta linguagem errada",
			markdown:   "```java\nclass Solution { int[] twoSum() {} }\n```",
			lang:       "python3",
			entryPoint: "twoSum",
			want:       nil,
		},
		{
			name:       "descarta bloco sem o entry point",
			markdown:   "```python3\nprint('complexidade O(n)')\n```",
			lang:       "python3",
			entryPoint: "twoSum",
			want:       nil,
		},
		{
			name:       "info vazio é aceito quando tem o entry point",
			markdown:   "```\nclass Solution:\n    def twoSum(self): pass\n```",
			lang:       "python3",
			entryPoint: "twoSum",
			want:       []string{"class Solution:\n    def twoSum(self): pass"},
		},
		{
			name:       "múltiplos blocos em ordem do documento",
			markdown:   "```python\ndef twoSum(a): return 1\n```\ntexto\n```python3\ndef twoSum(b): return 2\n```",
			lang:       "python3",
			entryPoint: "twoSum",
			want:       []string{"def twoSum(a): return 1", "def twoSum(b): return 2"},
		},
		{
			name:       "golang aceita alias go",
			markdown:   "```go\nfunc twoSum(nums []int) []int { return nil }\n```",
			lang:       "golang",
			entryPoint: "twoSum",
			want:       []string{"func twoSum(nums []int) []int { return nil }"},
		},
		{
			name:       "cpp aceita c++",
			markdown:   "```c++\nclass Solution { vector<int> twoSum(); };\n```",
			lang:       "cpp",
			entryPoint: "twoSum",
			want:       []string{"class Solution { vector<int> twoSum(); };"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCode(tt.markdown, tt.lang, tt.entryPoint)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d blocos %q, want %d %q", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("bloco %d:\n got %q\nwant %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeFenceInfo(t *testing.T) {
	cases := map[string]string{
		"python3":     "python3",
		"  Python3  ": "python3",
		"[Python3]":   "python3",
		"java [1]":    "java",
		"c++":         "c++",
		"":            "",
	}
	for in, want := range cases {
		if got := normalizeFenceInfo(in); got != want {
			t.Errorf("normalizeFenceInfo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlainContent(t *testing.T) {
	html := `<p>Given an array <code>nums</code>, return <strong>indices</strong>.</p>` +
		`<p>&nbsp;</p><pre><strong>Input:</strong> nums = [2,7]<br />x &lt; y</pre>`
	got := PlainContent(html)

	for _, want := range []string{"Given an array nums", "Input: nums = [2,7]", "x < y"} {
		if !strings.Contains(got, want) {
			t.Errorf("saída não contém %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<") && strings.Contains(got, ">") && strings.Contains(got, "strong") {
		t.Errorf("tags HTML sobraram:\n%s", got)
	}
}

func TestIndexLookup(t *testing.T) {
	idx := &Index{Entries: []IndexEntry{
		{FrontendID: 1, Slug: "two-sum"},
		{FrontendID: 42, Slug: "trapping-rain-water"},
	}}

	if e, ok := idx.Lookup("42"); !ok || e.Slug != "trapping-rain-water" {
		t.Errorf("Lookup por id falhou: %+v %t", e, ok)
	}
	if e, ok := idx.Lookup("Two-Sum"); !ok || e.FrontendID != 1 {
		t.Errorf("Lookup por slug (case-insensitive) falhou: %+v %t", e, ok)
	}
	if _, ok := idx.Lookup("9999"); ok {
		t.Error("Lookup deveria falhar para id inexistente")
	}
}

func TestQuestionEntryPoint(t *testing.T) {
	q := &Question{MetaData: `{"name":"twoSum","params":[],"return":{"type":"integer[]"}}`}
	if got := q.EntryPoint(); got != "twoSum" {
		t.Errorf("EntryPoint() = %q, want twoSum", got)
	}
	// Problemas de banco de dados não têm "name".
	db := &Question{MetaData: `{"mysql":["Create table ..."]}`}
	if got := db.EntryPoint(); got != "" {
		t.Errorf("EntryPoint() para problema de DB = %q, want vazio", got)
	}
}
