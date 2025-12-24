package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lincaiyong/agent"
	"github.com/lincaiyong/arg"
	"github.com/lincaiyong/log"
	"github.com/lincaiyong/uniapi/service/monica"
	"os"
	"strconv"
)

//go:embed task.txt
var taskTxt string

func setLogPath(index int) {
	logPath := fmt.Sprintf("./log/%d.log", index)
	_ = os.Remove(logPath)
	_ = log.SetLogPath(logPath)
}

func main() {
	arg.Parse()
	index, _ := strconv.Atoi(arg.KeyValueArg("index", ""))
	model := monica.ModelDeepSeekV31
	if arg.BoolArg("mini") {
		model = monica.ModelGPT41Mini
	}
	if arg.BoolArg("haiku") {
		model = monica.ModelClaude45Haiku
	}
	if arg.BoolArg("sonnet") {
		model = monica.ModelClaude4Sonnet
	}
	msg := arg.KeyValueArg("msg", "")
	limit, _ := strconv.Atoi(arg.KeyValueArg("limit", ""))
	chat := monica.ChatCompletion
	monica.Init(os.Getenv("MONICA_SESSION_ID"))
	workDir := "/tmp/sample"
	var worker *agent.Agent
	var err error
	if index > 0 {
		worker, err = agent.LoadAgent(fmt.Sprintf("./log/%d.json", index))
		if err != nil {
			log.ErrorLog("fail to load: %v", err)
			return
		}
	} else {
		_ = os.RemoveAll("./log")
		_ = os.Mkdir("./log", 0755)
		worker = agent.NewAgent(workDir, taskTxt, model)
	}
	if msg != "" {
		worker.Reminder = msg
	}
	index++
	setLogPath(index)
	ctx, cancel := context.WithCancel(context.Background())
	err = worker.Run(ctx, chat, func(a *agent.Agent) {
		b, _ := json.MarshalIndent(a, "", "\t")
		err = os.WriteFile(fmt.Sprintf("./log/%d.json", index), b, 0644)
		if err != nil {
			log.ErrorLog("fail to write: %v", err)
		}
		limit--
		if limit == 0 {
			cancel()
		}
		index++
		setLogPath(index)
	})
	if err != nil && !errors.Is(err, ctx.Err()) {
		log.ErrorLog("fail to run: %v", err)
		return
	}
	log.InfoLog("done")
}
