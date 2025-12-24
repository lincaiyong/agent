package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/lincaiyong/daemon/common"
	"github.com/lincaiyong/log"
	"os"
	"strings"
)

//go:embed prompts/system.txt
var systemTxt string

//go:embed prompts/tooluse.txt
var toolUseTxt string

func NewAgent(workDir, task, model string) *Agent {
	return &Agent{
		SystemPrompt:     systemTxt,
		ToolUsePrompt:    toolUseTxt,
		WorkDir:          workDir,
		TaskDescription:  task,
		PreviousActions:  nil,
		ToolUseHistory:   map[string]*ToolUse{},
		CurrentTasks:     nil,
		CurrentTasksById: map[int]*Task{},
		Model:            model,
	}
}

func LoadAgent(filePath string) (*Agent, error) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var agent Agent
	err = json.Unmarshal(b, &agent)
	if err != nil {
		return nil, err
	}
	agent.ToolUseHistory = map[string]*ToolUse{}
	agent.CurrentTasksById = map[int]*Task{}
	for _, action := range agent.PreviousActions {
		for _, use := range action.ToolUses {
			if _, ok := agent.ToolUseHistory[use.Key()]; !ok {
				agent.ToolUseHistory[use.Key()] = use
			}
		}
	}
	for _, task := range agent.CurrentTasks {
		agent.CurrentTasksById[task.Id] = task
	}
	return &agent, nil
}

type Agent struct {
	SystemPrompt     string    `json:"system_prompt,omitempty"`
	ToolUsePrompt    string    `json:"tool_use_prompt,omitempty"`
	WorkDir          string    `json:"work_dir,omitempty"`
	TaskDescription  string    `json:"task_description,omitempty"`
	PreviousActions  []*Action `json:"previous_actions,omitempty"`
	ToolUseHistory   map[string]*ToolUse
	CurrentTasks     []*Task `json:"current_tasks,omitempty"`
	CurrentTasksById map[int]*Task
	Model            string `json:"model,omitempty"`
	Reminder         string `json:"reminder,omitempty"`
}

type Action struct {
	Assistant string     `json:"assistant"`
	ToolUses  []*ToolUse `json:"tool_uses,omitempty"`
}

type Task struct {
	Id      int    `json:"id"`
	Status  string `json:"status"`
	Content string `json:"content"`
}

type ChatFn func(context.Context, string, string, func(string)) (string, error)
type TraceFn func(*Agent)

func (a *Agent) Run(ctx context.Context, chatFn ChatFn, traceFn TraceFn) error {
	for {
		var err error
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("context cancelled: %w", err)
		}
		query := a.compose()
		log.InfoLog("==========================================")
		log.InfoLog("(%d chars, ~ %d tokens)", len(query), len(query)/4)
		log.InfoLog(query)
		var resp string
		err = common.DoWithRetry(5, func() error {
			resp, err = chatFn(ctx, a.Model, query, func(s string) {
				fmt.Print(s)
			})
			return err
		})
		fmt.Println()
		if err != nil {
			return err
		}
		log.InfoLog("==========================================")
		log.InfoLog(resp)
		uses := ExtractToolUses(resp)
		for _, use := range uses {
			if a.ToolUseHistory[use.Key()] != nil {
				use.Error = "(forbidden: already called, do not call again)"
				continue
			}
			a.ToolUseHistory[use.Key()] = use
			if use.Name == "task" {
				task, ok := a.CurrentTasksById[use.Id]
				if !ok {
					task = &Task{
						Id:      use.Id,
						Status:  use.Status,
						Content: use.Args,
					}
					a.CurrentTasksById[use.Id] = task
					a.CurrentTasks = append(a.CurrentTasks, task)
				} else {
					if use.Status != "" {
						task.Status = use.Status
					}
					if use.Args != "" {
						task.Content = use.Args
					}
				}
			} else {
				err = use.Call(a.WorkDir)
				if err != nil {
					log.WarnLog("fail to call: %v", err)
					continue
				}
			}
			b, _ := json.MarshalIndent(use, "", "\t")
			log.InfoLog("==========================================")
			log.InfoLog(string(b))
		}
		action := &Action{Assistant: resp, ToolUses: uses}
		a.PreviousActions = append(a.PreviousActions, action)

		if traceFn != nil {
			traceFn(a)
		}

		if len(uses) == 0 && !strings.Contains(resp, "<tool_use ") {
			break
		}
	}
	return nil
}

func (a *Agent) compose() string {
	var sb strings.Builder
	sb.WriteString(a.SystemPrompt)
	sb.WriteString("\n")
	sb.WriteString(a.ToolUsePrompt)
	sb.WriteString(fmt.Sprintf("\n<work directory>\n%s\n</work directory>", a.WorkDir))
	sb.WriteString(fmt.Sprintf("\n<user>\n%s\n</user>\n", a.TaskDescription))
	if len(a.PreviousActions) > 0 {
		b, _ := json.MarshalIndent(a.PreviousActions, "", "\t")
		sb.WriteString(fmt.Sprintf("\n<previous actions>\n%s\n</previous actions>", string(b)))
	}
	if len(a.CurrentTasks) > 0 {
		b, _ := json.MarshalIndent(a.CurrentTasks, "", "\t")
		sb.WriteString(fmt.Sprintf("\n<current tasks>\n%s\n</current tasks>", string(b)))
	}
	if a.Reminder != "" {
		sb.WriteString(fmt.Sprintf("\n<reminder>\n%s\n</reminder>", a.Reminder))
		a.Reminder = ""
	}
	return sb.String()
}
