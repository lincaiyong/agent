package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lincaiyong/daemon/common"
	"github.com/lincaiyong/log"
	"github.com/lincaiyong/uniapi/service/monica"
	"os"
	"regexp"
	"strings"
)

var toolUseRegex *regexp.Regexp

type ToolUseCall struct {
	Name   string `json:"name"`
	Args   string `json:"args"`
	Error  string `json:",omitempty" json:"error"`
	Result string `json:",omitempty" json:"result"`
	key    string
}

func (call *ToolUseCall) Key() string {
	if call.key == "" {
		call.key = fmt.Sprintf("%s %s", call.Name, call.Args)
	}
	return call.key
}

func (call *ToolUseCall) Call(workDir string) error {
	if call.Name == "ls" || call.Name == "cat" || call.Name == "rg" {
		stdout, stderr, err := common.RunCommand(context.Background(), workDir, "bash", "-c", fmt.Sprintf("%s %s", call.Name, call.Args))
		if err != nil {
			call.Result = stderr
			call.Error = fmt.Sprintf("%v", err)
			return err
		}
		if len(stdout) > 1000 {
			stdout = stdout[:1000] + "..."
		}
		call.Result = stdout
		return nil
	} else {
		call.Error = fmt.Sprintf("unknown command %s", call.Name)
		return errors.New("unknown command")
	}
}

func extractToolUse(s string) []*ToolUseCall {
	if toolUseRegex == nil {
		toolUseRegex = regexp.MustCompile(`(?s)<tool>(.+?)</tool>`)
	}
	m := toolUseRegex.FindAllStringSubmatch(s, -1)
	if len(m) == 0 {
		return nil
	}
	var ret []*ToolUseCall
	for _, mm := range m {
		tool := strings.TrimSpace(mm[1])
		var toolCall ToolUseCall
		err := json.Unmarshal([]byte(tool), &toolCall)
		if err != nil {
			log.ErrorLog("fail to parse: %s", tool)
			continue
		}
		ret = append(ret, &toolCall)
	}
	return ret
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
	workDir := "/tmp/samples"
	systemPrompt := fmt.Sprintf(`<system>
You are a senior engineer, skilled at understanding Go code.
You can access following bash tools if you need:

<tools>
[
	{
		"tool": "cat",
		"description": "Concatenate and print files.",
		"usage": <tool>{"name":"cat", "args":"..."}</tool>
	},
	{
		"tool": "ls",
		"description": "List directory contents.",
		"usage": <tool>{"name":"ls", "args":"..."}</tool>
	},
	{
		"tool": "rg",
		"description": "ripgrep (rg) recursively searches the current directory for lines matching a regex pattern.",
		"usage": <tool>{"name":"rg", "args":"..."}</tool>",
		"examples": [
			<tool>{"name":"rg", "args":"-F -i 'main'"}</tool>,
			<tool>{"name":"rg", "args":"-w -m 50 -e '[Mm]ain'"}</tool>
		]
	}
]
</tools>

<tips>
1. 不要重复调用工具：你在调用工具前，总是应该在<tool_use_history></tool_use_history>中找历史记录。
2. 工具超过1000个字符的输出会被省略。
</tips>

Current working directory: %s.
</system>`, workDir)

	q := `<user>
当前目录是一个Go后台项目，分析POST /api/projects/:pid([0-9]+)/members/接口，是否存在SQL注入风险？
如果存在，给出完整的数据流（从接口到sink点）。
</user>`
	monica.Init(os.Getenv("MONICA_SESSION_ID"))
	_ = os.RemoveAll("./log")
	_ = os.Mkdir("./log", 0755)
	historyMap := make(map[string]*ToolUseCall)
	var history []*ToolUseCall
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
		model := "deepseek-v3.1"
		model = monica.ModelClaude4Sonnet
		ret, err := monica.ChatCompletion(context.Background(), model, query, func(s string) {
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
			err = call.Call(workDir)
			if err != nil {
				log.ErrorLog("fail to call: %v", err)
				continue
			}
			b, _ := json.MarshalIndent(call, "", "\t")
			log.InfoLog("==========================================")
			log.InfoLog(string(b))
			history = append(history, call)
		}
	}
	log.InfoLog("done")
}
