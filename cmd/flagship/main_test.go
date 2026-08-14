package main

import (
	"encoding/json"
	"testing"

	"github.com/fddhuwenjie/hwj-go-0004/flags"
)

func TestProcessWorkflow(t *testing.T) {
	input := []byte(`[
  {"op":"upsert","requestId":"r1","flag":{"key":"account","enabled":true,"default":"free","rules":[{"id":"pro","priority":1,"conditions":[{"attribute":"plan","operator":"equals","values":["pro"]}],"value":"pro"}]}},
  {"op":"upsert","flag":{"key":"launch","enabled":true,"default":"off","prerequisites":[{"key":"account","value":"pro"}],"rules":[{"id":"all","priority":1,"conditions":[{"attribute":"region","operator":"equals","values":["US"]}],"value":"on"}]}},
  {"op":"evaluate","key":"launch","context":{"subjectKey":"alice","attributes":{"plan":"pro","region":"US"}},"at":"2026-08-15T12:00:00Z"},
  {"op":"audit","audit":{"flagKey":"launch"}}
]`)
	commands, err := parse(input)
	if err != nil {
		t.Fatal(err)
	}
	serviceResults := make([]result, len(commands))
	service := flagsForTest()
	for i, command := range commands {
		serviceResults[i] = dispatch(service, command)
	}
	if !serviceResults[0].OK || !serviceResults[1].OK || !serviceResults[2].OK || !serviceResults[3].OK {
		t.Fatalf("results = %+v", serviceResults)
	}
	if serviceResults[2].Evaluation == nil || serviceResults[2].Evaluation.Value != "on" {
		t.Fatalf("evaluation = %+v", serviceResults[2].Evaluation)
	}
	if len(serviceResults[3].Audit) != 1 {
		t.Fatalf("audit = %+v", serviceResults[3].Audit)
	}
	if _, err := json.Marshal(serviceResults); err != nil {
		t.Fatal(err)
	}
}

func TestParseSingleAndRejectsEmpty(t *testing.T) {
	commands, err := parse([]byte(`{"op":"audit"}`))
	if err != nil || len(commands) != 1 {
		t.Fatalf("commands=%+v err=%v", commands, err)
	}
	if _, err := parse(nil); err == nil {
		t.Fatal("empty input should fail")
	}
	if _, err := parse([]byte(`null`)); err == nil {
		t.Fatal("null input should fail")
	}
}

func flagsForTest() *flags.Service { return flags.NewService() }
