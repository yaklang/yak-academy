package main

import "testing"

// extractCommand 与上游模拟接口同构：{"command":"..."} 的宽容提取。
// cat 白名单匹配依赖它，行为必须稳定。
func TestExtractCommand(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"plain ls", `{"command":"ls"}`, "ls"},
		{"cat flag", `{"command":"cat flag.txt"}`, "cat flag.txt"},
		{"cat abs path", `{"command":"cat /flag.txt"}`, "cat /flag.txt"},
		{"cat bare", `{"command":"cat flag"}`, "cat flag"},
		{"spaces", `{ "command" : "cat flag.txt" }`, "cat flag.txt"},
		{"missing field", `{"cmd":"ls"}`, ""},
		{"empty", `{"command":""}`, ""},
		{"other keys around", `{"a":1,"command":"ls","b":2}`, "ls"},
		{"non json", `command=ls`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractCommand([]byte(c.body)); got != c.want {
				t.Fatalf("extractCommand(%s) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

func TestJSONEscape(t *testing.T) {
	got := mustJSON(map[string]string{"command": `cat "x"`, "result": "a\nb"})
	want := `{"command":"cat \"x\"","result":"a\nb"}`
	if got != want {
		t.Fatalf("mustJSON = %s, want %s", got, want)
	}
}