package agentruntime_test

import (
	"strings"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestCurrentDecisionParserAcceptsV6AndRejectsV5Tools(t *testing.T) {
	t.Parallel()

	currentTools := []string{
		"canvas.read",
		"canvas.apply_ops",
		"assets.read",
		"assets.publish",
		"media.generate",
		"skills.load",
	}
	for _, toolName := range currentTools {
		payload := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"call-current","toolName":"` + toolName + `","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
		decision, err := agentruntime.ParseModelDecision(payload)
		if err != nil {
			t.Fatalf("current tool %q was rejected: %v", toolName, err)
		}
		if decision.ToolCall == nil || decision.ToolCall.ToolName != agentruntime.ToolName(toolName) {
			t.Fatalf("decision for %q = %#v", toolName, decision)
		}
	}

	retiredTools := []string{
		"skill.load",
		"specialist.delegate",
		"vision.analyze",
		"canvas.project",
		"media.assemble",
	}
	for _, toolName := range retiredTools {
		payload := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"call-retired","toolName":"` + toolName + `","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
		if _, err := agentruntime.ParseModelDecision(payload); err == nil {
			t.Fatalf("retired v5 tool %q remained executable", toolName)
		}
	}
}

func TestParseModelDecisionAcceptsStrictFinalAndToolCall(t *testing.T) {
	finalJSON := []byte(`{"kind":"final","final":{"message":"已完成","expectedDelivery":{"kind":"answer","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"}]}}}`)
	decision, err := agentruntime.ParseModelDecision(finalJSON)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentruntime.DecisionFinal || decision.Final == nil || decision.Final.ExpectedDelivery.Kind != agentruntime.DeliveryAnswer {
		t.Fatalf("final decision = %#v", decision)
	}

	toolJSON := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"canvas.read","actionVersion":1,"arguments":{"canvasId":"canvas-1"},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	decision, err = agentruntime.ParseModelDecision(toolJSON)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentruntime.DecisionToolCall || decision.ToolCall == nil || decision.ToolCall.ToolName != agentruntime.ToolCanvasRead {
		t.Fatalf("tool decision = %#v", decision)
	}
}

func TestParseModelDecisionForToolSchemaHardCutsCurrentTools(t *testing.T) {
	legacy := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"call-legacy","toolName":"production.plan","actionVersion":1,"arguments":{"planKey":"plan-1"},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	production := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"call-v5","toolName":"specialist.delegate","actionVersion":1,"arguments":{"specialistKey":"narrative"},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)
	current := []byte(`{"kind":"tool_call","toolCall":{"toolCallId":"call-v6","toolName":"canvas.read","actionVersion":1,"arguments":{"canvasId":"canvas-1"},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`)

	if _, err := agentruntime.ParseModelDecisionForToolSchema(legacy, agentruntime.CurrentToolSchemaVersion); err == nil {
		t.Fatal("current schema accepted retired legacy tool")
	}
	if _, err := agentruntime.ParseModelDecisionForToolSchema(production, agentruntime.CurrentToolSchemaVersion); err == nil {
		t.Fatal("current schema accepted retired production tool")
	}
	decision, err := agentruntime.ParseModelDecisionForToolSchema(current, agentruntime.CurrentToolSchemaVersion)
	if err != nil {
		t.Fatalf("current schema rejected cloud tool: %v", err)
	}
	if decision.ToolCall == nil || decision.ToolCall.ToolName != agentruntime.ToolCanvasRead {
		t.Fatalf("current decision = %#v", decision)
	}
	if _, err := agentruntime.ParseModelDecisionForToolSchema(production, agentruntime.ProductionToolSchemaVersion); err == nil {
		t.Fatal("retired production schema remained executable after hard cut")
	}
	if _, err := agentruntime.ParseModelDecisionForToolSchema(production, 999); err == nil {
		t.Fatal("unknown tool schema was accepted")
	}
	if _, err := agentruntime.ParseModelDecisionForToolSchema(legacy, agentruntime.LegacyToolSchemaVersion); err == nil {
		t.Fatal("retired schema remained executable after hard cut")
	}
}

func TestParseModelDecisionAcceptsStrictClarificationRequest(t *testing.T) {
	payload := []byte(`{
		"kind":"clarification_request",
		"clarification":{
			"requestId":"vehicle-ad-brief",
			"questions":[
				{"id":"vehicle","type":"single_choice","prompt":"请选择车型","options":[{"id":"x5","label":"宝马 X5"},{"id":"model-3","label":"特斯拉 Model 3"}],"allowCustomAnswer":true},
				{"id":"channels","type":"multi_choice","prompt":"请选择投放渠道","options":[{"id":"douyin","label":"抖音"},{"id":"xiaohongshu","label":"小红书"}]},
				{"id":"requirements","type":"free_text","prompt":"还有哪些要求？"}
			],
			"expectedDelivery":{"kind":"mixed","targetCanvasId":"canvas-1","requiredArtifacts":["text"],"completionCriteria":[{"fact":"final_message"},{"fact":"canvas_revision"}]}
		}
	}`)
	decision, err := agentruntime.ParseModelDecision(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentruntime.DecisionClarificationRequest || decision.Clarification == nil {
		t.Fatalf("clarification decision = %#v", decision)
	}
	if decision.Clarification.RequestID != "vehicle-ad-brief" || len(decision.Clarification.Questions) != 3 || !decision.Clarification.Questions[0].AllowCustomAnswer {
		t.Fatalf("clarification payload = %#v", decision.Clarification)
	}
}

func TestParseModelDecisionRejectsInvalidClarificationRequest(t *testing.T) {
	delivery := `"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}`
	cases := map[string]string{
		"no questions":               `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[],` + delivery + `}}`,
		"too many questions":         `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"free_text","prompt":"1"},{"id":"q2","type":"free_text","prompt":"2"},{"id":"q3","type":"free_text","prompt":"3"},{"id":"q4","type":"free_text","prompt":"4"}],` + delivery + `}}`,
		"duplicate question id":      `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"free_text","prompt":"1"},{"id":"q1","type":"free_text","prompt":"2"}],` + delivery + `}}`,
		"unknown question type":      `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"boolean","prompt":"1"}],` + delivery + `}}`,
		"choice has one option":      `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"single_choice","prompt":"1","options":[{"id":"o1","label":"1"}]}],` + delivery + `}}`,
		"choice duplicate option":    `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"multi_choice","prompt":"1","options":[{"id":"o1","label":"1"},{"id":"o1","label":"2"}]}],` + delivery + `}}`,
		"free text has options":      `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"free_text","prompt":"1","options":[{"id":"o1","label":"1"},{"id":"o2","label":"2"}]}],` + delivery + `}}`,
		"free text allows custom":    `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"free_text","prompt":"1","allowCustomAnswer":true}],` + delivery + `}}`,
		"unknown question field":     `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"free_text","prompt":"1","extra":true}],` + delivery + `}}`,
		"missing expected delivery":  `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"free_text","prompt":"1"}]}}`,
		"multiple decision payloads": `{"kind":"clarification_request","clarification":{"requestId":"request-1","questions":[{"id":"q1","type":"free_text","prompt":"1"}],` + delivery + `},"final":{"message":"done","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := agentruntime.ParseModelDecision([]byte(payload)); err == nil {
				t.Fatal("invalid clarification request was accepted")
			}
		})
	}
}

func TestParseModelDecisionFailsClosed(t *testing.T) {
	cases := map[string]string{
		"unknown decision":      `{"kind":"route"}`,
		"unknown field":         `{"kind":"final","extra":true,"final":{"message":"ok","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`,
		"two documents":         `{"kind":"final","final":{"message":"ok","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}} {}`,
		"unknown tool":          `{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"shell.exec","actionVersion":1,"arguments":{}}}`,
		"retired canvas tool":   `{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"canvas.project","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`,
		"retired generation":    `{"kind":"tool_call","toolCall":{"toolCallId":"call-1","toolName":"generation.submit","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`,
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
