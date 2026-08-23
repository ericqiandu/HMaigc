package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type productionCanvasBinding struct {
	ArtifactID string `json:"artifactId"`
	NodeID     string `json:"nodeId"`
}

type productionCanvasNode struct {
	ID       string                       `json:"id"`
	Type     string                       `json:"type"`
	Title    string                       `json:"title"`
	Position agentCanvasPosition          `json:"position"`
	Width    float64                      `json:"width"`
	Height   float64                      `json:"height"`
	Metadata productionCanvasNodeMetadata `json:"metadata"`
}

type productionCanvasNodeMetadata struct {
	Content         string                      `json:"content,omitempty"`
	ComposerContent string                      `json:"composerContent,omitempty"`
	Prompt          string                      `json:"prompt,omitempty"`
	Status          string                      `json:"status"`
	ErrorDetails    string                      `json:"errorDetails,omitempty"`
	GenerationMode  string                      `json:"generationMode,omitempty"`
	StorageKey      string                      `json:"storageKey,omitempty"`
	MimeType        string                      `json:"mimeType,omitempty"`
	DurationMS      int64                       `json:"durationMs,omitempty"`
	WorkflowKind    string                      `json:"workflowKind"`
	WorkflowTitle   string                      `json:"workflowTitle"`
	ShotIndex       int                         `json:"shotIndex,omitempty"`
	TaskID          string                      `json:"taskId,omitempty"`
	TaskStatus      string                      `json:"taskStatus,omitempty"`
	TaskProgress    int                         `json:"taskProgress,omitempty"`
	TaskStage       string                      `json:"taskStage,omitempty"`
	Storyboard      *productionCanvasStoryboard `json:"storyboard,omitempty"`
}

type productionCanvasStoryboard struct {
	Rows             []productionCanvasStoryboardRow `json:"rows"`
	VisibleColumns   []string                        `json:"visibleColumns"`
	ReferenceNodeIDs []string                        `json:"referenceNodeIds"`
}

type productionCanvasStoryboardRow struct {
	ID                    string                                   `json:"id"`
	ShotNumber            int                                      `json:"shotNumber"`
	DurationSeconds       float64                                  `json:"durationSeconds"`
	Deliverables          []agentruntime.ProductionShotDeliverable `json:"deliverables"`
	PlotDescription       string                                   `json:"plotDescription"`
	Dialogue              string                                   `json:"dialogue"`
	Characters            []string                                 `json:"characters"`
	ShotSize              string                                   `json:"shotSize"`
	Emotion               string                                   `json:"emotion"`
	LightingAndAtmosphere string                                   `json:"lightingAndAtmosphere"`
	AudioEffects          string                                   `json:"audioEffects"`
	Camera                string                                   `json:"camera"`
	Motion                string                                   `json:"motion"`
	TimeBeats             string                                   `json:"timeBeats"`
	ImageGenerationPrompt string                                   `json:"imageGenerationPrompt"`
	VideoMotionPrompt     string                                   `json:"videoMotionPrompt"`
	NegativePrompt        string                                   `json:"negativePrompt"`
	ReferenceNodeIDs      []string                                 `json:"referenceNodeIds"`
	ImageNodeID           string                                   `json:"imageNodeId,omitempty"`
	VideoNodeID           string                                   `json:"videoNodeId,omitempty"`
	Status                string                                   `json:"status"`
}

type productionCanvasConnection struct {
	ID           string `json:"id"`
	FromNodeID   string `json:"fromNodeId"`
	ToNodeID     string `json:"toNodeId"`
	FromHandleID string `json:"fromHandleId,omitempty"`
}

func buildProductionCanvasPatch(
	plan model.AgentProductionPlanVersion,
	artifacts []model.AgentProductionArtifact,
	resources map[string]model.Resource,
) (CanvasMutationPatch, []productionCanvasBinding, error) {
	draft, err := decodeStoredAgentProductionPlan(plan)
	if err != nil {
		return CanvasMutationPatch{}, nil, err
	}
	references := draft.References
	shots := draft.Shots
	if strings.TrimSpace(plan.PlanKey) == "" || plan.Version < 1 || len(shots) == 0 {
		return CanvasMutationPatch{}, nil, errors.New("production canvas plan is invalid")
	}
	artifactsByRole := make(map[string]model.AgentProductionArtifact, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.PlanKey != plan.PlanKey || artifact.PlanVersionID != plan.ID || artifact.PlanVersion != plan.Version {
			return CanvasMutationPatch{}, nil, errors.New("production canvas artifact scope conflict")
		}
		role := productionArtifactRole(artifact.ReferenceKey, artifact.ShotKey, artifact.Kind)
		if _, exists := artifactsByRole[role]; exists {
			return CanvasMutationPatch{}, nil, errors.New("production canvas artifact role is duplicated")
		}
		artifactsByRole[role] = artifact
	}
	script, exists := artifactsByRole[productionArtifactRole("", "", model.AgentProductionArtifactScript)]
	if !exists {
		return CanvasMutationPatch{}, nil, errors.New("production canvas script artifact is missing")
	}

	orderedArtifacts := []model.AgentProductionArtifact{script}
	for _, reference := range references {
		artifact, referenceExists := artifactsByRole[productionArtifactRole(reference.ReferenceKey, "", model.AgentProductionArtifactReferenceImage)]
		if !referenceExists {
			return CanvasMutationPatch{}, nil, errors.New("production canvas reference artifact is missing")
		}
		orderedArtifacts = append(orderedArtifacts, artifact)
	}
	for _, shot := range shots {
		if shot.Delivers(agentruntime.ProductionShotDeliverableStoryboardImage) {
			image, exists := artifactsByRole[productionArtifactRole("", shot.ShotKey, model.AgentProductionArtifactStoryboardImage)]
			if !exists {
				return CanvasMutationPatch{}, nil, errors.New("production canvas storyboard artifact is missing")
			}
			orderedArtifacts = append(orderedArtifacts, image)
		}
		if shot.Delivers(agentruntime.ProductionShotDeliverableVideoClip) {
			video, exists := artifactsByRole[productionArtifactRole("", shot.ShotKey, model.AgentProductionArtifactVideoClip)]
			if !exists {
				return CanvasMutationPatch{}, nil, errors.New("production canvas video artifact is missing")
			}
			orderedArtifacts = append(orderedArtifacts, video)
		}
	}
	if len(orderedArtifacts) != len(artifacts) {
		return CanvasMutationPatch{}, nil, errors.New("production canvas artifacts contain unsupported roles")
	}

	nodeByArtifact := make(map[string]string, len(orderedArtifacts))
	bindings := make([]productionCanvasBinding, 0, len(orderedArtifacts))
	for _, artifact := range orderedArtifacts {
		nodeID := productionCanvasNodeID(plan, artifact)
		nodeByArtifact[artifact.ID] = nodeID
		bindings = append(bindings, productionCanvasBinding{ArtifactID: artifact.ID, NodeID: nodeID})
	}
	referenceNodeByKey := make(map[string]string, len(references))
	for _, reference := range references {
		artifact := artifactsByRole[productionArtifactRole(reference.ReferenceKey, "", model.AgentProductionArtifactReferenceImage)]
		referenceNodeByKey[reference.ReferenceKey] = nodeByArtifact[artifact.ID]
	}

	rows := make([]productionCanvasStoryboardRow, 0, len(shots))
	for _, shot := range shots {
		shotReferenceNodeIDs := make([]string, 0, len(shot.ReferenceKeys))
		for _, referenceKey := range shot.ReferenceKeys {
			nodeID, referenceExists := referenceNodeByKey[referenceKey]
			if !referenceExists {
				return CanvasMutationPatch{}, nil, errors.New("production canvas shot reference is missing")
			}
			shotReferenceNodeIDs = append(shotReferenceNodeIDs, nodeID)
		}
		imageNodeID := ""
		if shot.Delivers(agentruntime.ProductionShotDeliverableStoryboardImage) {
			image := artifactsByRole[productionArtifactRole("", shot.ShotKey, model.AgentProductionArtifactStoryboardImage)]
			imageNodeID = nodeByArtifact[image.ID]
		}
		videoNodeID := ""
		if shot.Delivers(agentruntime.ProductionShotDeliverableVideoClip) {
			video := artifactsByRole[productionArtifactRole("", shot.ShotKey, model.AgentProductionArtifactVideoClip)]
			videoNodeID = nodeByArtifact[video.ID]
		}
		rows = append(rows, productionCanvasStoryboardRow{
			ID: shot.ShotKey, ShotNumber: shot.Order, DurationSeconds: float64(shot.DurationMS) / 1000,
			Deliverables:    append([]agentruntime.ProductionShotDeliverable(nil), shot.Deliverables...),
			PlotDescription: shot.ScriptText, Characters: []string{}, ShotSize: "", Emotion: "", LightingAndAtmosphere: "",
			AudioEffects: "", Camera: "", Motion: "", TimeBeats: "",
			ImageGenerationPrompt: shot.ImagePrompt, VideoMotionPrompt: shot.VideoPrompt, NegativePrompt: "",
			ReferenceNodeIDs: shotReferenceNodeIDs, ImageNodeID: imageNodeID, VideoNodeID: videoNodeID, Status: "success",
		})
	}
	scriptNode := productionCanvasNode{
		ID: nodeByArtifact[script.ID], Type: "script", Title: plan.Title,
		Position: agentCanvasPosition{X: 0, Y: 0}, Width: 920, Height: 360,
		Metadata: productionCanvasNodeMetadata{
			ComposerContent: plan.Script, Status: productionArtifactCanvasStatus(script.Status), WorkflowKind: "script", WorkflowTitle: plan.Title,
			Storyboard: &productionCanvasStoryboard{
				Rows: rows, VisibleColumns: []string{"shotNumber", "durationSeconds", "plotDescription", "dialogue", "imageGenerationPrompt", "videoMotionPrompt"},
				ReferenceNodeIDs: orderedReferenceNodeIDs(references, referenceNodeByKey),
			},
		},
	}
	nodes := []productionCanvasNode{scriptNode}
	connections := make([]productionCanvasConnection, 0, len(references)+len(shots)*2)
	for index, reference := range references {
		artifact := artifactsByRole[productionArtifactRole(reference.ReferenceKey, "", model.AgentProductionArtifactReferenceImage)]
		referenceNode, err := productionReferenceCanvasNode(plan, reference, artifact, resources, 500, float64(index)*280)
		if err != nil {
			return CanvasMutationPatch{}, nil, err
		}
		nodes = append(nodes, referenceNode)
		connections = append(connections, productionCanvasConnection{
			ID: productionCanvasConnectionID(plan, scriptNode.ID, referenceNode.ID), FromNodeID: scriptNode.ID, ToNodeID: referenceNode.ID,
		})
	}
	for _, shot := range shots {
		var imageNode *productionCanvasNode
		if shot.Delivers(agentruntime.ProductionShotDeliverableStoryboardImage) {
			imageArtifact := artifactsByRole[productionArtifactRole("", shot.ShotKey, model.AgentProductionArtifactStoryboardImage)]
			projected, err := productionMediaCanvasNode(plan, shot, imageArtifact, resources, 1000, float64(shot.Order-1)*300)
			if err != nil {
				return CanvasMutationPatch{}, nil, err
			}
			imageNode = &projected
			nodes = append(nodes, projected)
			connections = append(connections, productionCanvasConnection{
				ID: productionCanvasConnectionID(plan, scriptNode.ID, projected.ID), FromNodeID: scriptNode.ID, ToNodeID: projected.ID, FromHandleID: "row:" + shot.ShotKey,
			})
			for _, referenceKey := range shot.ReferenceKeys {
				connections = append(connections, productionCanvasConnection{
					ID:         productionCanvasConnectionID(plan, referenceNodeByKey[referenceKey], projected.ID),
					FromNodeID: referenceNodeByKey[referenceKey], ToNodeID: projected.ID,
				})
			}
		}
		if shot.Delivers(agentruntime.ProductionShotDeliverableVideoClip) {
			videoArtifact := artifactsByRole[productionArtifactRole("", shot.ShotKey, model.AgentProductionArtifactVideoClip)]
			videoX := 1000.0
			if imageNode != nil {
				videoX = 1440
			}
			videoNode, err := productionMediaCanvasNode(plan, shot, videoArtifact, resources, videoX, float64(shot.Order-1)*300)
			if err != nil {
				return CanvasMutationPatch{}, nil, err
			}
			nodes = append(nodes, videoNode)
			connection := productionCanvasConnection{
				ID: productionCanvasConnectionID(plan, scriptNode.ID, videoNode.ID), FromNodeID: scriptNode.ID, ToNodeID: videoNode.ID, FromHandleID: "row:" + shot.ShotKey,
			}
			if imageNode != nil {
				connection = productionCanvasConnection{
					ID: productionCanvasConnectionID(plan, imageNode.ID, videoNode.ID), FromNodeID: imageNode.ID, ToNodeID: videoNode.ID,
				}
			}
			connections = append(connections, connection)
		}
	}
	patch := CanvasMutationPatch{UpsertNodes: make([]json.RawMessage, 0, len(nodes)), UpsertConnections: make([]json.RawMessage, 0, len(connections))}
	for _, node := range nodes {
		encoded, err := json.Marshal(node)
		if err != nil {
			return CanvasMutationPatch{}, nil, err
		}
		patch.UpsertNodes = append(patch.UpsertNodes, encoded)
	}
	for _, connection := range connections {
		encoded, err := json.Marshal(connection)
		if err != nil {
			return CanvasMutationPatch{}, nil, err
		}
		patch.UpsertConnections = append(patch.UpsertConnections, encoded)
	}
	return patch, bindings, nil
}

func orderedReferenceNodeIDs(references []agentruntime.ReferenceAssetDraft, nodeByKey map[string]string) []string {
	result := make([]string, 0, len(references))
	for _, reference := range references {
		result = append(result, nodeByKey[reference.ReferenceKey])
	}
	return result
}

func productionReferenceCanvasNode(
	plan model.AgentProductionPlanVersion,
	reference agentruntime.ReferenceAssetDraft,
	artifact model.AgentProductionArtifact,
	resources map[string]model.Resource,
	x float64,
	y float64,
) (productionCanvasNode, error) {
	metadata := productionCanvasNodeMetadata{
		Prompt: reference.ImagePrompt, ComposerContent: reference.ImagePrompt, Status: productionArtifactCanvasStatus(artifact.Status),
		GenerationMode: "image", WorkflowKind: "reference", WorkflowTitle: reference.Title,
		TaskID: artifact.TaskID, TaskStatus: string(artifact.Status), TaskProgress: productionArtifactProgress(artifact.Status),
		TaskStage: productionArtifactStage(artifact.Status), ErrorDetails: artifact.LastErrorCode,
	}
	if artifact.ResourceID != "" {
		resource, exists := resources[artifact.ResourceID]
		if exists && resource.Status == model.ResourceStatusReady && resource.Kind == "image" {
			metadata.Content = "/api/resources/" + resource.ID + "/file"
			metadata.StorageKey = "resource:" + resource.ID
			metadata.MimeType = resource.MimeType
		} else if artifact.Status == model.AgentProductionArtifactSucceeded || artifact.Status == model.AgentProductionArtifactCommitted {
			return productionCanvasNode{}, errors.New("successful production reference resource is not ready")
		}
	}
	return productionCanvasNode{
		ID: productionCanvasNodeID(plan, artifact), Type: "image", Title: reference.Title,
		Position: agentCanvasPosition{X: x, Y: y}, Width: 340, Height: 240, Metadata: metadata,
	}, nil
}

func productionMediaCanvasNode(plan model.AgentProductionPlanVersion, shot agentruntime.ShotPlanDraft, artifact model.AgentProductionArtifact, resources map[string]model.Resource, x float64, y float64) (productionCanvasNode, error) {
	nodeType := "image"
	titleSuffix := "分镜图"
	prompt := shot.ImagePrompt
	width, height := 340.0, 240.0
	if artifact.Kind == model.AgentProductionArtifactVideoClip {
		nodeType, titleSuffix, prompt, width, height = "video", "视频", shot.VideoPrompt, 420, 236
	}
	metadata := productionCanvasNodeMetadata{
		Prompt: prompt, ComposerContent: prompt, Status: productionArtifactCanvasStatus(artifact.Status),
		GenerationMode: nodeType, WorkflowKind: "shot", WorkflowTitle: "镜头 " + strconv.Itoa(shot.Order) + " " + titleSuffix,
		ShotIndex: shot.Order, TaskID: artifact.TaskID, TaskStatus: string(artifact.Status),
		TaskProgress: productionArtifactProgress(artifact.Status), TaskStage: productionArtifactStage(artifact.Status), ErrorDetails: artifact.LastErrorCode,
	}
	if artifact.ResourceID != "" {
		resource, exists := resources[artifact.ResourceID]
		if exists && resource.Status == model.ResourceStatusReady && resource.Kind == nodeType {
			metadata.Content = "/api/resources/" + resource.ID + "/file"
			metadata.StorageKey = "resource:" + resource.ID
			metadata.MimeType = resource.MimeType
			metadata.DurationMS = resource.DurationMs
		} else if artifact.Status == model.AgentProductionArtifactSucceeded || artifact.Status == model.AgentProductionArtifactCommitted {
			return productionCanvasNode{}, errors.New("successful production artifact resource is not ready")
		}
	}
	return productionCanvasNode{
		ID: productionCanvasNodeID(plan, artifact), Type: nodeType, Title: "镜头 " + strconv.Itoa(shot.Order) + " · " + titleSuffix,
		Position: agentCanvasPosition{X: x, Y: y}, Width: width, Height: height, Metadata: metadata,
	}, nil
}

func productionArtifactRole(referenceKey string, shotKey string, kind model.AgentProductionArtifactKind) string {
	return string(kind) + ":" + strings.TrimSpace(referenceKey) + ":" + strings.TrimSpace(shotKey)
}

func productionCanvasNodeID(plan model.AgentProductionPlanVersion, artifact model.AgentProductionArtifact) string {
	return productionCanvasStableID("production-node", plan.PlanKey, strconv.Itoa(plan.Version), artifact.ID)
}

func productionCanvasConnectionID(plan model.AgentProductionPlanVersion, fromNodeID string, toNodeID string) string {
	return productionCanvasStableID("production-edge", plan.PlanKey, strconv.Itoa(plan.Version), fromNodeID, toNodeID)
}

func productionCanvasStableID(parts ...string) string {
	normalized := append([]string(nil), parts...)
	for index := range normalized {
		normalized[index] = strings.TrimSpace(normalized[index])
	}
	digest := sha256.Sum256([]byte(strings.Join(normalized, ":")))
	return normalized[0] + "-" + hex.EncodeToString(digest[:16])
}

func productionArtifactCanvasStatus(status model.AgentProductionArtifactStatus) string {
	switch status {
	case model.AgentProductionArtifactPlanned:
		return "idle"
	case model.AgentProductionArtifactAwaitingApproval, model.AgentProductionArtifactQueued, model.AgentProductionArtifactRunning:
		return "loading"
	case model.AgentProductionArtifactSucceeded, model.AgentProductionArtifactCommitted:
		return "success"
	case model.AgentProductionArtifactFailed:
		return "error"
	default:
		return "error"
	}
}

func productionArtifactProgress(status model.AgentProductionArtifactStatus) int {
	switch status {
	case model.AgentProductionArtifactPlanned, model.AgentProductionArtifactAwaitingApproval:
		return 0
	case model.AgentProductionArtifactQueued:
		return 5
	case model.AgentProductionArtifactRunning:
		return 50
	case model.AgentProductionArtifactSucceeded, model.AgentProductionArtifactCommitted:
		return 100
	default:
		return 0
	}
}

func productionArtifactStage(status model.AgentProductionArtifactStatus) string {
	switch status {
	case model.AgentProductionArtifactPlanned:
		return "等待生成"
	case model.AgentProductionArtifactAwaitingApproval:
		return "等待费用确认"
	case model.AgentProductionArtifactQueued:
		return "等待队列调度"
	case model.AgentProductionArtifactRunning:
		return "正在生成"
	case model.AgentProductionArtifactSucceeded, model.AgentProductionArtifactCommitted:
		return "已完成"
	case model.AgentProductionArtifactFailed:
		return "生成失败"
	default:
		return "状态异常"
	}
}
