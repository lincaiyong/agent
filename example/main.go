package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/lincaiyong/log"
	"github.com/lincaiyong/uniapi/service/monica"
	"os"
	"regexp"
	"strings"
)

var toolUseRegex *regexp.Regexp

type ToolUseCall struct {
	Name string
	Args string
	key  string
}

func (c *ToolUseCall) Key() string {
	if c.key == "" {
		c.key = fmt.Sprintf("%s %s", c.Name, c.Args)
	}
	return c.key
}

func extractToolUse(s string) []*ToolUseCall {
	if toolUseRegex == nil {
		toolUseRegex = regexp.MustCompile(`<tool name="(.+?)">(.+?)</tool>`)
	}
	m := toolUseRegex.FindAllStringSubmatch(s, -1)
	if len(m) == 0 {
		return nil
	}
	var ret []*ToolUseCall
	for _, mm := range m {
		call := &ToolUseCall{
			Name: mm[1],
			Args: mm[2],
		}
		ret = append(ret, call)
	}
	return ret
}

func read(filePath string) string {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return "<FAIL TO READ>"
	}
	lines := bytes.Split(b, []byte("\n"))
	var sb strings.Builder
	for i, line := range lines {
		sb.WriteString(fmt.Sprintf("|%5d|", i+1))
		sb.Write(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

func ls(dirPath string) string {
	items, err := os.ReadDir(dirPath)
	if err != nil {
		return ""
	}
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.Name()
	}
	return strings.Join(result, ",")
}

type ToolUseResult struct {
	ToolUse string `json:"tool_use"`
	Result  string `json:"result"`
}

func main() {
	err := os.WriteFile("/tmp/test.py", []byte(`import bar
def foo(x, y):
	print(bar.add(x,y))
if __name__ == '__main__':
	foo(3, 4)`), 0644)
	if err != nil {
		log.ErrorLog("fail to write: %v", err)
		return
	}
	err = os.WriteFile("/tmp/bar.py", []byte("def add(a, b): return a * b"), 0644)
	if err != nil {
		log.ErrorLog("fail to write: %v", err)
		return
	}
	systemPrompt := `<system>
You are a senior engineer, skilled at understanding Python code.
You know how to trace variables and functions to find their definitions in order to answer questions.
You can access following tools if you need:
[
	{
		"tool": "read",
		"description": "Read file from file system. Line numbers will be prefixed to each file line using the |%5d| format specifier for right-aligned numerical presentation.",
		"usage": "<tool name=\"read\">/path/to/file</tool>"
	},
	{
		"tool": "ls",
		"description": "List directory.",
		"usage": "<tool name=\"ls\">/path/to/file</tool>"
	}
]
</system>`

	q := `<user>
what can i see after run "python3 /tmp/test.py"?
</user>`
	monica.Init(os.Getenv("MONICA_SESSION_ID"))
	_ = os.RemoveAll("./log")
	_ = os.Mkdir("./log", 0755)
	historyMap := make(map[string]*ToolUseCall)
	var history []*ToolUseResult
	var i int
	for {
		i++
		err = log.SetLogPath(fmt.Sprintf("./log/%d.log", i))
		if err != nil {
			log.ErrorLog("fail to write: %v", err)
			return
		}
		var query string
		if history == nil {
			query = fmt.Sprintf("%s\n%s", systemPrompt, q)
		} else {
			b, _ := json.MarshalIndent(history, "", "\t")
			query = fmt.Sprintf("%s\n<tool_use_history>\n%s\n</tool_use_history>\n%s", systemPrompt, string(b), q)
		}
		log.InfoLog("==========================================")
		log.InfoLog(query)
		ret, err := monica.ChatCompletion(context.Background(), "deepseek-v3.1", query, func(s string) {
			fmt.Print(s)
		})
		fmt.Println()
		if err != nil {
			log.ErrorLog("fail to chat: %v", err)
			return
		}
		log.InfoLog("==========================================")
		log.InfoLog(ret)
		calls := extractToolUse(ret)
		if len(calls) == 0 {
			break
		}
		for _, call := range calls {
			if historyMap[call.Key()] != nil {
				continue
			}
			historyMap[call.Key()] = call
			var result string
			if call.Name == "read" {
				result = read(call.Args)
			} else if call.Name == "ls" {
				result = ls(call.Args)
			}
			ur := &ToolUseResult{
				ToolUse: fmt.Sprintf("%s %s", call.Name, call.Args),
				Result:  result,
			}
			b, _ := json.MarshalIndent(ur, "", "\t")
			log.InfoLog("==========================================")
			log.InfoLog(string(b))
			history = append(history, ur)
		}
	}
	log.InfoLog("done")
}
