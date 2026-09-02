package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuildAgentRuntimeDecisionDiagnosticRecordsStructureWithoutPrompt(t *testing.T) {
	payload := []byte(`{
		"kind":"tool_call",
		"toolCall":{
			"toolCallId":"generate-image-1",
			"toolName":"media.generate",
			"actionVersion":1,
			"unexpectedToolField":true,
			"arguments":{
				"mediaKind":"image",
				"modelRecordId":"runtime-image-model",
				"modelKey":"gpt-image-2",
				"parameters":{"prompt":"private user prompt","ratio":"1:1","count":1},
				"sourceResourceIds":[],
				"targetCanvasNodeId":"",
				"clientRequestId":"generate-image-1"
			},
			"expectedDelivery":{"kind":"image_asset","requiredArtifacts":["image"],"completionCriteria":[{"fact":"artifact","artifact":"image"}]}
		}
	}`)

	diagnostic := buildAgentRuntimeDecisionDiagnostic(payload, errors.New("agent tool call arguments are invalid"))
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	if strings.Contains(serialized, "private user prompt") || strings.Contains(serialized, `"prompt"`) {
		t.Fatalf("diagnostic leaked prompt content or key: %s", serialized)
	}
	if diagnostic.DecisionKind != "tool_call" || diagnostic.ToolName != "media.generate" {
		t.Fatalf("decision identity = %#v", diagnostic)
	}
	if diagnostic.StrictDecodeProblem != "unknown_field:unexpectedToolField" {
		t.Fatalf("strict decode problem = %q", diagnostic.StrictDecodeProblem)
	}
	if strings.Join(diagnostic.DecisionKeys, ",") != "kind,toolCall" ||
		strings.Join(diagnostic.ToolCallKeys, ",") != "actionVersion,arguments,expectedDelivery,toolCallId,toolName,unexpectedToolField" ||
		strings.Join(diagnostic.ExpectedDeliveryKeys, ",") != "completionCriteria,kind,requiredArtifacts" {
		t.Fatalf("decision structure = %#v", diagnostic)
	}
	if diagnostic.DeliveryKind != "image_asset" || strings.Join(diagnostic.RequiredArtifacts, ",") != "image" ||
		strings.Join(diagnostic.CriterionFacts, ",") != "artifact:image" {
		t.Fatalf("delivery structure = %#v", diagnostic)
	}
	if diagnostic.Media == nil || !diagnostic.Media.ModelRecordIDPresent || !diagnostic.Media.ModelRecordIDNonEmpty ||
		!diagnostic.Media.TargetCanvasNodeIDPresent || diagnostic.Media.TargetCanvasNodeIDNonEmpty ||
		diagnostic.Media.SourceResourceCount != 0 {
		t.Fatalf("media structure = %#v", diagnostic.Media)
	}
	if strings.Join(diagnostic.ArgumentKeys, ",") != "clientRequestId,mediaKind,modelKey,modelRecordId,parameters,sourceResourceIds,targetCanvasNodeId" {
		t.Fatalf("argument keys = %#v", diagnostic.ArgumentKeys)
	}
	if strings.Join(diagnostic.ParameterKeys, ",") != "count,ratio" {
		t.Fatalf("parameter keys = %#v", diagnostic.ParameterKeys)
	}
}
