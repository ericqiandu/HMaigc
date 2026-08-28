package agentruntime

import "testing"

func TestDecisionStreamObserverReleasesOnlyFinalMessage(t *testing.T) {
	observer := NewDecisionStreamObserver()
	chunks := []string{`{"final":{"message":"你`, `好\n世`, `界","expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}},`, `"kind":"final"}`}
	visible := ""
	for _, chunk := range chunks {
		delta, err := observer.Push(chunk)
		if err != nil {
			t.Fatal(err)
		}
		visible += delta
	}
	decision, err := observer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if visible != "你好\n世界" || decision.Final == nil || decision.Final.Message != visible {
		t.Fatalf("visible = %q, decision = %#v", visible, decision)
	}
}

func TestDecisionStreamObserverNeverReleasesNonFinalFields(t *testing.T) {
	observer := NewDecisionStreamObserver()
	delta, err := observer.Push(`{"kind":"tool_call","toolCall":{"arguments":{"message":"secret"}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if delta != "" {
		t.Fatalf("visible tool delta = %q", delta)
	}
	if _, err := observer.Finish(); err == nil {
		t.Fatal("invalid tool decision unexpectedly passed strict validation")
	}
}

func TestDecisionStreamObserverEmitsMonotonicDeltasAfterFinalKindIsKnown(t *testing.T) {
	observer := NewDecisionStreamObserver()
	first, err := observer.Push(`{"kind":"final","final":{"message":"第一`)
	if err != nil || first != "第一" {
		t.Fatalf("first delta = %q, error = %v", first, err)
	}
	second, err := observer.Push(`段第二`)
	if err != nil || second != "段第二" {
		t.Fatalf("second delta = %q, error = %v", second, err)
	}
}

func TestDecisionStreamObserverUsesFrozenToolSchema(t *testing.T) {
	observer := NewDecisionStreamObserverForToolSchema(ProductionToolSchemaVersion)
	legacy := `{"kind":"tool_call","toolCall":{"toolCallId":"legacy","toolName":"production.plan","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	if _, err := observer.Push(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Finish(); err == nil {
		t.Fatal("production observer accepted retired legacy tool")
	}

	observer = NewDecisionStreamObserverForToolSchema(ProductionToolSchemaVersion)
	production := `{"kind":"tool_call","toolCall":{"toolCallId":"delegate","toolName":"specialist.delegate","actionVersion":1,"arguments":{},"expectedDelivery":{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}}}`
	if _, err := observer.Push(production); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Finish(); err != nil {
		t.Fatalf("production observer rejected generic tool: %v", err)
	}
}
