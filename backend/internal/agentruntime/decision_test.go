package agentruntime_test

import (
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestParseModelDecisionAcceptsStrictFinalAndToolCall(t *testing.T) {
	finalJSON := []byte(`{"kind":"final","final":{"message":"已完成","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	decision, err := agentruntime.ParseModelDecision(finalJSON)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentruntime.DecisionFinal || decision.Final == nil || decision.Final.ExpectedDelivery.Kind != agentruntime.DeliveryAnswer {
		t.Fatalf("final decision = %#v", decision)
	}

	toolJSON := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"canvas.read_state","actionVersion":1,"arguments":{"revision":3},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	decision, err = agentruntime.ParseModelDecision(toolJSON)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentruntime.DecisionToolCall || decision.ToolCall == nil || decision.ToolCall.ToolName != agentruntime.ToolCanvasReadState {
		t.Fatalf("tool decision = %#v", decision)
	}
}

func TestParseModelDecisionFailsClosed(t *testing.T) {
	cases := map[string]string{
		"unknown decision":      `{"kind":"route"}`,
		"unknown field":         `{"kind":"final","extra":true,"final":{"message":"ok","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`,
		"two documents":         `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}} {}`,
		"unknown tool":          `{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"shell.exec","actionVersion":1,"arguments":{}}}`,
		"missing criteria":      `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer"}}}`,
		"both payloads":         `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}},"toolCall":{"toolCallId":"call-1","toolName":"canvas.read_state","actionVersion":1,"arguments":{}}}`,
		"invalid arguments":     `{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"canvas.read_state","actionVersion":1,"arguments":null}}`,
		"missing tool delivery": `{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"canvas.read_state","actionVersion":1,"arguments":{}}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := agentruntime.ParseModelDecision([]byte(payload)); err == nil {
				t.Fatal("invalid decision was accepted")
			}
		})
	}

	longMessage := `{"kind":"final","final":{"message":"` + strings.Repeat("x", 32*1024+1) + `","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	if _, err := agentruntime.ParseModelDecision([]byte(longMessage)); err == nil {
		t.Fatal("oversized final message was accepted")
	}
}
