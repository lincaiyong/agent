package agent

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/lincaiyong/daemon/common"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type ToolUse struct {
	Name string `json:"name"`

	// for read: file path
	// for write: text in json format
	// for ls, rg: parameters
	// for task: description
	Args string `json:"args"`

	// for read
	StartLine int `json:"start_line,omitempty"`
	LineCount int `json:"line_count,omitempty"`

	// for write
	FilePath string `json:"file_path,omitempty"`

	// for task
	Status string `json:"status,omitempty"`
	Id     int    `json:"id,omitempty"`

	Error  string `json:"error,omitempty"`
	Result string `json:"result,omitempty"`
	key    string
}

func calcMd5(s string) string {
	hash := md5.Sum([]byte(s))
	return hex.EncodeToString(hash[:])
}

func (t *ToolUse) Key() string {
	switch t.Name {
	case "ls", "rg":
		t.key = fmt.Sprintf("%s-%s", t.Name, t.Args)
	case "read":
		t.key = fmt.Sprintf("%s-%d-%d-%s", t.Name, t.StartLine, t.LineCount, t.Args)
	case "write":
		t.key = fmt.Sprintf("%s-%s-%s", t.Name, t.FilePath, calcMd5(t.Args))
	case "task":
		t.key = fmt.Sprintf("%s-%d-%s-%s", t.Name, t.Id, t.Status, calcMd5(t.Args))
	}
	return t.key
}

func (t *ToolUse) checkFilePath(workDir, filePath string) (string, error) {
	if strings.Contains(filePath, "..") {
		return "", fmt.Errorf("invalid path('..' is not allowed): %s", filePath)
	}
	if strings.HasPrefix(filePath, "/") {
		if !strings.HasPrefix(filePath, workDir) {
			return "", fmt.Errorf("invalid path(access outside the working directory is not allowed): %s", filePath)
		}
	} else {
		filePath = filepath.Join(workDir, filePath)
	}
	return filePath, nil
}

func (t *ToolUse) Call(workDir string) error {
	if t.Name == "write" {
		var data any
		err := json.Unmarshal([]byte(t.Args), &data)
		if err != nil {
			t.Error = fmt.Sprintf("invalid json: %v", err)
			return err
		}
		targetPath, err := t.checkFilePath(workDir, t.FilePath)
		if err != nil {
			t.Error = fmt.Sprintf("%v", err)
			return err
		}
		b, _ := json.MarshalIndent(data, "", "\t")
		err = os.WriteFile(targetPath, b, 0644)
		if err != nil {
			t.Error = fmt.Sprintf("fail to write: %v", err)
			return err
		} else {
			t.Result = fmt.Sprintf("written to: %s", targetPath)
			return nil
		}
	} else if t.Name == "ls" || t.Name == "rg" {
		args := strings.Fields(t.Args)
		stdout, stderr, err := common.RunCommand(context.Background(), workDir, t.Name, args...)
		if err != nil {
			t.Error = fmt.Sprintf("fail to run: %v, %s", err, stderr)
			return err
		}
		lines := strings.Split(stdout, "\n")
		var sb strings.Builder
		for i, line := range lines {
			if i >= 100 {
				sb.WriteString("...(more than 100 lines, truncated)\n")
				break
			}
			if len(line) > 1000 {
				line = line[:1000] + "...(more than 1000 chars, truncated)"
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		t.Result = sb.String()
	} else if t.Name == "read" {
		targetPath, err := t.checkFilePath(workDir, t.Args)
		if err != nil {
			t.Error = fmt.Sprintf("%v", err)
			return err
		}
		b, err := os.ReadFile(targetPath)
		if err != nil {
			t.Error = fmt.Sprintf("fail to read: %v", err)
			return err
		}
		startLineIdx := 0
		if t.StartLine > 0 {
			startLineIdx = t.StartLine - 1
		}
		lineCount := 100
		if t.LineCount > 0 {
			lineCount = t.LineCount
		}
		var sb strings.Builder
		lines := bytes.Split(b, []byte("\n"))
		for i := startLineIdx; i < len(lines); i++ {
			if i >= startLineIdx+lineCount {
				break
			}
			line := lines[i]
			if len(line) > 1000 {
				line = line[:1000]
				line = append(line, []byte("...(more than 1000 chars, truncated)")...)
			}
			sb.WriteString(fmt.Sprintf("|%5d|", i+1))
			sb.Write(line)
			sb.WriteString("\n")
		}
		t.Result = sb.String()
	}
	return nil
}

var toolUseRegex *regexp.Regexp
var toolUseExtraRegex *regexp.Regexp

func extractToolUseExtra(s string) map[string]string {
	result := make(map[string]string)
	s = strings.TrimSpace(s)
	if s == "" {
		return result
	}
	if toolUseExtraRegex == nil {
		toolUseExtraRegex = regexp.MustCompile(`^([a-z_]+)="(.+)"$`)
	}
	items := strings.Fields(s)
	for _, item := range items {
		m := toolUseExtraRegex.FindStringSubmatch(item)
		if m != nil {
			result[m[1]] = m[2]
		}
	}
	return result
}

func ExtractToolUses(s string) []*ToolUse {
	if toolUseRegex == nil {
		toolUseRegex = regexp.MustCompile(`(?s)<tool_use name="(read|write|ls|rg|task)"(.*?)>(.+?)</tool_use>`)
	}
	m := toolUseRegex.FindAllStringSubmatch(s, -1)
	if len(m) == 0 {
		return nil
	}
	var ret []*ToolUse
	for _, mm := range m {
		var use ToolUse
		use.Name = mm[1]
		use.Args = mm[3]
		extra := extractToolUseExtra(mm[2])
		use.FilePath = extra["file_path"]
		use.Status = extra["status"]
		use.Id, _ = strconv.Atoi(extra["id"])
		use.StartLine, _ = strconv.Atoi(extra["start_line"])
		use.LineCount, _ = strconv.Atoi(extra["line_count"])
		ret = append(ret, &use)
	}
	return ret
}
