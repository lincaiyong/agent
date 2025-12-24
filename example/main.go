package main

import (
	"context"
	_ "embed"
	"encoding/json"
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
	chat := monica.ChatCompletion
	monica.Init(os.Getenv("MONICA_SESSION_ID"))
	workDir := "/tmp/sample"
	var worker *agent.Agent
	var err error
	if index > 0 {
		worker, err = agent.LoadAgent(fmt.Sprintf("./log/%d.txt", index))
		if err != nil {
			log.ErrorLog("fail to load: %v", err)
			return
		}
	} else {
		_ = os.RemoveAll("./log")
		_ = os.Mkdir("./log", 0755)
		worker = agent.NewAgent(workDir, taskTxt, model)
	}
	err = worker.Run(context.Background(), chat, func(a *agent.Agent) {
		index++
		b, _ := json.MarshalIndent(a, "", "\t")
		err := os.WriteFile(fmt.Sprintf("./log/%d.log", index), b, 0644)
		if err != nil {
			log.ErrorLog("fail to write: %v", err)
		}
	})
	if err != nil {
		log.ErrorLog("fail to run: %v", err)
		return
	}
	log.InfoLog("done")
}
