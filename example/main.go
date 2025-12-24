package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lincaiyong/daemon/common"
	"github.com/lincaiyong/log"
	"github.com/lincaiyong/uniapi/service/monica"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var toolUseRegex *regexp.Regexp

type ToolUseCall struct {
	Name      string `json:"name"`
	Args      string `json:"args"`
	StartLine int    `json:"start_line,omitempty"`
	LineCount int    `json:"line_count,omitempty"`
	Json      string `json:"json,omitempty"`
	Error     string `json:"error,omitempty"`
	Result    string `json:"result,omitempty"`
	key       string
}

func (call *ToolUseCall) Key() string {
	if call.key == "" {
		call.key = fmt.Sprintf("%s-%s-%d-%d-%s", call.Name, call.Args, call.StartLine, call.LineCount, call.Json)
	}
	return call.key
}

func (call *ToolUseCall) Call(workDir string) error {
	if call.Name == "ls" || call.Name == "cat" || call.Name == "rg" {
		if call.Name == "cat" && call.Json != "" {
			targetPath := call.Args
			if !strings.HasPrefix(targetPath, "/") {
				targetPath = filepath.Join(workDir, targetPath)
			}
			var data any
			err := json.Unmarshal([]byte(call.Json), &data)
			if err != nil {
				call.Error = fmt.Sprintf("invalid json: %v", err)
				return err
			}
			err = os.WriteFile(targetPath, []byte(call.Json), 0644)
			if err != nil {
				call.Error = fmt.Sprintf("fail to write: %v", err)
				return err
			} else {
				call.Result = fmt.Sprintf("written to: %s", targetPath)
				return nil
			}
		}
		stdout, stderr, err := common.RunCommand(context.Background(), workDir, "bash", "-c", fmt.Sprintf("%s %s", call.Name, call.Args))
		if err != nil {
			call.Result = stderr
			call.Error = fmt.Sprintf("%v", err)
			return err
		}
		startLine := call.StartLine
		lineCount := 100
		if call.LineCount > 0 {
			lineCount = call.LineCount
		}
		lines := strings.Split(stdout, "\n")
		var sb strings.Builder
		for i, line := range lines {
			if len(line) > 1000 {
				line = line[:1000] + "...(more than 1000 chars, truncated)"
			}
			if i >= startLine && i < startLine+lineCount {
				sb.WriteString(fmt.Sprintf("|%5d|", i+1))
				sb.WriteString(line)
				sb.WriteString("\n")
			}
		}
		call.Result = sb.String()
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

type Chat struct {
	Response string         `json:"response"`
	Calls    []*ToolUseCall `json:"calls,omitempty"`
}

//go:embed task.txt
var taskTxt string

func main() {
	workDir := "/tmp/sample"
	systemPrompt := fmt.Sprintf(`<system>
You are a senior engineer, skilled at understanding Go code.
You can access following bash tools if you need:

<tools>
[
	{
		"tool": "cat",
		"description": "Concatenate and print files.",
		"usage": <tool>{"name":"cat", "args":"...", "start_line":\d+, "line_count":\d+}</tool>,
		"comment": "start_line和end_line是可选的（默认start_line=1，line_count=100）。"
		"examples": [
			<tool>{"name":"cat", "args":"test.txt"}</tool>,
			<tool>{"name":"cat", "args":"result.txt", "json":"..."}</tool> // 写入文件使用json参数，必须是合法的json字符串
		]
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
1. 不要重复调用工具：你在调用工具前，总是应该先看下历史记录中是否已经有过一样的调用。
2. 工具超过1000个字符的输出会被省略。
</tips>

Current working directory: %s.

</system>`, workDir)

	q := fmt.Sprintf(`<user>
%s
</user>`, taskTxt)
	monica.Init(os.Getenv("MONICA_SESSION_ID"))
	_ = os.RemoveAll("./log")
	_ = os.Mkdir("./log", 0755)
	called := make(map[string]*ToolUseCall)
	var history []*Chat
	var i int
	var err error
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
			query = fmt.Sprintf("%s\n%s\n<history>\n%s\n</history>", systemPrompt, q, string(b))
		}
		log.InfoLog("==========================================")
		log.InfoLog("%d chars, ~ %d tokens", len(query), len(query)/4)
		log.InfoLog(query)
		model := "deepseek-v3.1"
		//model = monica.ModelClaude4Sonnet
		var resp string
		err = common.DoWithRetry(5, func() error {
			resp, err = monica.ChatCompletion(context.Background(), model, query, func(s string) {
				fmt.Print(s)
			})
			return err
		})
		fmt.Println()
		if err != nil {
			log.ErrorLog("fail to chat: %v", err)
			return
		}
		log.InfoLog("==========================================")
		log.InfoLog(resp)
		calls := extractToolUse(resp)
		if len(calls) == 0 {
			break
		}
		for _, call := range calls {
			if called[call.Key()] != nil {
				call.Error = "(forbidden: 已经调用过，不要再次调用！！)"
				continue
			}
			called[call.Key()] = call
			err = call.Call(workDir)
			if err != nil {
				log.ErrorLog("fail to call: %v", err)
				continue
			}
			b, _ := json.MarshalIndent(call, "", "\t")
			log.InfoLog("==========================================")
			log.InfoLog(string(b))
		}
		chat := &Chat{
			Response: resp,
			Calls:    calls,
		}
		history = append(history, chat)
	}
	log.InfoLog("done")
}
