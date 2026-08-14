package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fddhuwenjie/hwj-go-0004/flags"
)

type command struct {
	Op        string           `json:"op"`
	RequestID string           `json:"requestId,omitempty"`
	Flag      flags.Flag       `json:"flag,omitempty"`
	Flags     []flags.Flag     `json:"flags,omitempty"`
	Key       string           `json:"key,omitempty"`
	Context   flags.Context    `json:"context,omitempty"`
	At        time.Time        `json:"at,omitempty"`
	Audit     flags.AuditQuery `json:"audit,omitempty"`
}

type result struct {
	Index      int                `json:"index"`
	Op         string             `json:"op"`
	OK         bool               `json:"ok"`
	Error      string             `json:"error,omitempty"`
	Flag       *flags.Flag        `json:"flag,omitempty"`
	Flags      []flags.Flag       `json:"flags,omitempty"`
	Evaluation *flags.Evaluation  `json:"evaluation,omitempty"`
	Audit      []flags.AuditEvent `json:"audit,omitempty"`
}

func main() {
	input := flag.String("in", "-", "input JSON file, or - for stdin")
	output := flag.String("out", "-", "output JSON file, or - for stdout")
	flag.Parse()
	if err := run(*input, *output); err != nil {
		fmt.Fprintln(os.Stderr, "flagship:", err)
		os.Exit(1)
	}
}

func run(input, output string) error {
	data, err := readFile(input)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	commands, err := parse(data)
	if err != nil {
		return err
	}
	service := flags.NewService()
	results := make([]result, len(commands))
	for i, command := range commands {
		results[i] = dispatch(service, command)
		results[i].Index = i
	}
	payload, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if output == "-" {
		_, err = os.Stdout.Write(payload)
		return err
	}
	return os.WriteFile(output, payload, 0o644)
}

func parse(data []byte) ([]command, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty input")
	}
	switch data[0] {
	case '[':
		var batch []command
		if err := json.Unmarshal(data, &batch); err == nil && batch != nil {
			return batch, nil
		}
	case '{':
		var single command
		if err := json.Unmarshal(data, &single); err == nil {
			return []command{single}, nil
		}
	}
	return nil, errors.New("input must be a JSON command or command array")
}

func dispatch(service *flags.Service, command command) result {
	result := result{Op: command.Op}
	switch command.Op {
	case "upsert":
		stored, err := service.Upsert(command.Flag, command.RequestID)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Flag = &stored
	case "import":
		stored, err := service.Import(command.Flags, command.RequestID)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Flags = stored
	case "evaluate":
		evaluation, err := service.Evaluate(command.Key, command.Context, command.At)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Evaluation = &evaluation
	case "audit":
		events, err := service.QueryAudit(command.Audit)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Audit = events
	default:
		result.Error = fmt.Sprintf("unknown op: %q", command.Op)
		return result
	}
	result.OK = true
	return result
}

func readFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
