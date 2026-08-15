package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"infinite-canvas/backend/internal/model"
)

const agentCanvasReadNodeLimit = 200
const agentCanvasReadConnectionLimit = 400
const agentCanvasTextFactLimit = 500

type agentCanvasReadStateArguments struct {
	ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
}

type agentCanvasReadSelectionArguments struct{}

type agentCanvasDocumentFacts struct {
	Nodes       []agentCanvasNodeDocument       `json:"nodes"`
	Connections []agentCanvasConnectionDocument `json:"connections"`
}

type agentCanvasNodeDocument struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Title    string                  `json:"title"`
	Position agentCanvasPosition     `json:"position"`
	Width    float64                 `json:"width"`
	Height   float64                 `json:"height"`
	Metadata agentCanvasNodeMetadata `json:"metadata"`
}

type agentCanvasPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type agentCanvasNodeMetadata struct {
	Content             string   `json:"content,omitempty"`
	Prompt              string   `json:"prompt,omitempty"`
	ComposerContent     string   `json:"composerContent,omitempty"`
	Status              string   `json:"status,omitempty"`
	TaskID              string   `json:"taskId,omitempty"`
	TaskStatus          string   `json:"taskStatus,omitempty"`
	TaskProgress        *float64 `json:"taskProgress,omitempty"`
	TaskStage           string   `json:"taskStage,omitempty"`
	StorageKey          string   `json:"storageKey,omitempty"`
	ErrorDetails        string   `json:"errorDetails,omitempty"`
	GenerationMode      string   `json:"generationMode,omitempty"`
	Model               string   `json:"model,omitempty"`
	Size                string   `json:"size,omitempty"`
	WorkflowKind        string   `json:"workflowKind,omitempty"`
	WorkflowTitle       string   `json:"workflowTitle,omitempty"`
	WorkflowDescription string   `json:"workflowDescription,omitempty"`
	ChapterID           string   `json:"chapterId,omitempty"`
	ChapterTitle        string   `json:"chapterTitle,omitempty"`
	ShotIndex           *int     `json:"shotIndex,omitempty"`
}

type agentCanvasConnectionDocument struct {
	ID           string `json:"id"`
	FromNodeID   string `json:"fromNodeId"`
	ToNodeID     string `json:"toNodeId"`
	FromHandleID string `json:"fromHandleId,omitempty"`
	ToHandleID   string `json:"toHandleId,omitempty"`
}

type agentCanvasNodeFact struct {
	ID       string                      `json:"id"`
	Type     string                      `json:"type"`
	Title    string                      `json:"title"`
	Position agentCanvasPosition         `json:"position"`
	Width    float64                     `json:"width"`
	Height   float64                     `json:"height"`
	Metadata agentCanvasNodeMetadataFact `json:"metadata"`
}

type agentCanvasNodeMetadataFact struct {
	Content             string   `json:"content,omitempty"`
	Prompt              string   `json:"prompt,omitempty"`
	Status              string   `json:"status,omitempty"`
	TaskID              string   `json:"taskId,omitempty"`
	TaskStatus          string   `json:"taskStatus,omitempty"`
	TaskProgress        *float64 `json:"taskProgress,omitempty"`
	TaskStage           string   `json:"taskStage,omitempty"`
	StorageKey          string   `json:"storageKey,omitempty"`
	ErrorDetails        string   `json:"errorDetails,omitempty"`
	GenerationMode      string   `json:"generationMode,omitempty"`
	Model               string   `json:"model,omitempty"`
	Size                string   `json:"size,omitempty"`
	WorkflowKind        string   `json:"workflowKind,omitempty"`
	WorkflowTitle       string   `json:"workflowTitle,omitempty"`
	WorkflowDescription string   `json:"workflowDescription,omitempty"`
	ChapterID           string   `json:"chapterId,omitempty"`
	ChapterTitle        string   `json:"chapterTitle,omitempty"`
	ShotIndex           *int     `json:"shotIndex,omitempty"`
}

type agentCanvasReadStateResult struct {
	CanvasID             string                          `json:"canvasId"`
	Title                string                          `json:"title"`
	Revision             int64                           `json:"revision"`
	NodeCount            int                             `json:"nodeCount"`
	NodesTruncated       bool                            `json:"nodesTruncated"`
	Nodes                []agentCanvasNodeFact           `json:"nodes"`
	ConnectionCount      int                             `json:"connectionCount"`
	ConnectionsTruncated bool                            `json:"connectionsTruncated"`
	Connections          []agentCanvasConnectionDocument `json:"connections"`
}

type agentCanvasReadSelectionResult struct {
	CanvasID string                `json:"canvasId"`
	Revision int64                 `json:"revision"`
	Nodes    []agentCanvasNodeFact `json:"nodes"`
}

func executeAgentCanvasReadState(project *model.CanvasProject, rawArguments json.RawMessage) ([]byte, error) {
	arguments, err := decodeAgentCanvasReadStateArguments(rawArguments)
	if err != nil {
		return nil, err
	}
	if arguments.ExpectedRevision != nil && *arguments.ExpectedRevision != project.Revision {
		return nil, errors.New("agent canvas revision conflict")
	}
	document, err := agentCanvasDocument(project.PayloadJSON)
	if err != nil {
		return nil, err
	}
	nodeLimit := min(len(document.Nodes), agentCanvasReadNodeLimit)
	connectionLimit := min(len(document.Connections), agentCanvasReadConnectionLimit)
	nodes, err := compactAgentCanvasNodes(document.Nodes[:nodeLimit])
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentCanvasReadStateResult{
		CanvasID: project.ID, Title: project.Title, Revision: project.Revision,
		NodeCount: len(document.Nodes), NodesTruncated: nodeLimit < len(document.Nodes), Nodes: nodes,
		ConnectionCount: len(document.Connections), ConnectionsTruncated: connectionLimit < len(document.Connections),
		Connections: document.Connections[:connectionLimit],
	})
}

func executeAgentCanvasReadSelection(project *model.CanvasProject, selection *AgentCanvasSelectionFacts) ([]byte, error) {
	if selection == nil || selection.Revision != project.Revision || len(selection.NodeIDs) == 0 || len(selection.NodeIDs) > agentCanvasReadNodeLimit {
		return nil, errors.New("agent canvas selection facts are invalid")
	}
	document, err := agentCanvasDocument(project.PayloadJSON)
	if err != nil {
		return nil, err
	}
	nodesByID := make(map[string]agentCanvasNodeDocument, len(document.Nodes))
	for _, node := range document.Nodes {
		node.ID = strings.TrimSpace(node.ID)
		if node.ID == "" || node.Type == "" || nodesByID[node.ID].ID != "" {
			return nil, errors.New("agent canvas node facts are invalid")
		}
		nodesByID[node.ID] = node
	}
	selected := make([]agentCanvasNodeDocument, 0, len(selection.NodeIDs))
	seen := make(map[string]bool, len(selection.NodeIDs))
	for _, nodeID := range selection.NodeIDs {
		nodeID = strings.TrimSpace(nodeID)
		node, ok := nodesByID[nodeID]
		if nodeID == "" || seen[nodeID] || !ok {
			return nil, errors.New("agent canvas selection facts are invalid")
		}
		seen[nodeID] = true
		selected = append(selected, node)
	}
	nodes, err := compactAgentCanvasNodes(selected)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentCanvasReadSelectionResult{CanvasID: project.ID, Revision: project.Revision, Nodes: nodes})
}

func compactAgentCanvasNodes(nodes []agentCanvasNodeDocument) ([]agentCanvasNodeFact, error) {
	result := make([]agentCanvasNodeFact, 0, len(nodes))
	for _, node := range nodes {
		node.ID = strings.TrimSpace(node.ID)
		node.Type = strings.TrimSpace(node.Type)
		if node.ID == "" || node.Type == "" {
			return nil, errors.New("agent canvas node facts are invalid")
		}
		prompt := node.Metadata.Prompt
		if strings.TrimSpace(prompt) == "" {
			prompt = node.Metadata.ComposerContent
		}
		result = append(result, agentCanvasNodeFact{
			ID: node.ID, Type: node.Type, Title: node.Title, Position: node.Position, Width: node.Width, Height: node.Height,
			Metadata: agentCanvasNodeMetadataFact{
				Content: truncateAgentCanvasText(node.Metadata.Content), Prompt: truncateAgentCanvasText(prompt),
				Status: node.Metadata.Status, TaskID: node.Metadata.TaskID, TaskStatus: node.Metadata.TaskStatus,
				TaskProgress: node.Metadata.TaskProgress, TaskStage: node.Metadata.TaskStage, StorageKey: node.Metadata.StorageKey,
				ErrorDetails: truncateAgentCanvasText(node.Metadata.ErrorDetails), GenerationMode: node.Metadata.GenerationMode,
				Model: node.Metadata.Model, Size: node.Metadata.Size, WorkflowKind: node.Metadata.WorkflowKind,
				WorkflowTitle: node.Metadata.WorkflowTitle, WorkflowDescription: truncateAgentCanvasText(node.Metadata.WorkflowDescription),
				ChapterID: node.Metadata.ChapterID, ChapterTitle: node.Metadata.ChapterTitle, ShotIndex: node.Metadata.ShotIndex,
			},
		})
	}
	return result, nil
}

func truncateAgentCanvasText(value string) string {
	runes := []rune(value)
	if len(runes) <= agentCanvasTextFactLimit {
		return value
	}
	return string(runes[:agentCanvasTextFactLimit])
}

func agentCanvasDocument(payload string) (agentCanvasDocumentFacts, error) {
	var document agentCanvasDocumentFacts
	if err := json.Unmarshal([]byte(payload), &document); err != nil || document.Nodes == nil || document.Connections == nil {
		return agentCanvasDocumentFacts{}, errors.New("agent canvas document facts are invalid")
	}
	return document, nil
}

func decodeAgentCanvasReadStateArguments(payload []byte) (agentCanvasReadStateArguments, error) {
	var target agentCanvasReadStateArguments
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return agentCanvasReadStateArguments{}, errors.New("agent tool arguments are invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentCanvasReadStateArguments{}, errors.New("agent tool arguments must contain one object")
	}
	return target, nil
}

func decodeAgentCanvasReadSelectionArguments(payload []byte) error {
	var target agentCanvasReadSelectionArguments
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&target); err != nil {
		return errors.New("agent selection tool arguments are invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("agent selection tool arguments must contain one object")
	}
	return nil
}
