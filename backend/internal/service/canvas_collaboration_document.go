package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	maxCanvasMutationBytes       = 2 << 20
	maxCanvasMutationItems       = 1000
	maxCanvasDocumentPayloadSize = 5 << 20
)

func validateCanvasMutationPatch(patch CanvasMutationPatch) error {
	itemCount := len(patch.UpsertNodes) + len(patch.DeleteNodeIDs) + len(patch.UpsertConnections) + len(patch.DeleteConnectionIDs)
	if itemCount == 0 && patch.Document == nil {
		return BadAuthRequest("画布变更内容不能为空")
	}
	if itemCount > maxCanvasMutationItems {
		return BadAuthRequest("单次画布变更项目不能超过 1000 个")
	}
	if err := validateRawEntities(patch.UpsertNodes, "节点"); err != nil {
		return err
	}
	if err := validateRawEntities(patch.UpsertConnections, "连线"); err != nil {
		return err
	}
	if err := validateEntityIDs(patch.DeleteNodeIDs, "节点"); err != nil {
		return err
	}
	if err := validateEntityIDs(patch.DeleteConnectionIDs, "连线"); err != nil {
		return err
	}
	if patch.Document != nil {
		if patch.Document.Title != nil {
			title := strings.TrimSpace(*patch.Document.Title)
			if title == "" || len([]rune(title)) > 120 {
				return BadAuthRequest("画布名称不能为空且最多 120 个字符")
			}
		}
		if patch.Document.BackgroundMode != nil {
			mode := *patch.Document.BackgroundMode
			if mode != "dots" && mode != "lines" && mode != "blank" {
				return BadAuthRequest("画布背景模式无效")
			}
		}
		if patch.Document.DirectorScenes != nil && !json.Valid(*patch.Document.DirectorScenes) {
			return BadAuthRequest("导演台数据格式无效")
		}
		if patch.Document.Viewport != nil && !validCanvasViewportPatch(*patch.Document.Viewport) {
			return BadAuthRequest("画布视口数据无效")
		}
	}
	return nil
}

func validateRawEntities(items []json.RawMessage, label string) error {
	seen := map[string]struct{}{}
	for _, raw := range items {
		var identity struct {
			ID string `json:"id"`
		}
		if len(raw) == 0 || len(raw) > maxCanvasMutationBytes || json.Unmarshal(raw, &identity) != nil {
			return BadAuthRequest(label + "数据格式无效")
		}
		id := strings.TrimSpace(identity.ID)
		if id == "" || len(id) > 120 {
			return BadAuthRequest(label + " ID 无效")
		}
		if _, exists := seen[id]; exists {
			return BadAuthRequest(label + " ID 重复")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateEntityIDs(ids []string, label string) error {
	seen := map[string]struct{}{}
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" || len(id) > 120 {
			return BadAuthRequest(label + " ID 无效")
		}
		if _, exists := seen[id]; exists {
			return BadAuthRequest(label + " ID 重复")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func applyCanvasMutationPatch(
	payloadJSON string,
	currentTitle string,
	patch CanvasMutationPatch,
) (string, string, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payloadJSON), &document); err != nil {
		return "", "", fmt.Errorf("画布数据损坏：%w", err)
	}
	nodes, err := applyEntityPatch(document["nodes"], patch.UpsertNodes, patch.DeleteNodeIDs)
	if err != nil {
		return "", "", err
	}
	connections, err := applyEntityPatch(document["connections"], patch.UpsertConnections, patch.DeleteConnectionIDs)
	if err != nil {
		return "", "", err
	}
	if err := validateConnectionTypes(nodes, connections); err != nil {
		return "", "", err
	}
	if err := validateConnectionEndpoints(nodes, connections); err != nil {
		return "", "", err
	}
	document["nodes"], _ = json.Marshal(nodes)
	document["connections"], _ = json.Marshal(connections)
	title := currentTitle
	if patch.Document != nil {
		if patch.Document.Title != nil {
			title = strings.TrimSpace(*patch.Document.Title)
			document["title"], _ = json.Marshal(title)
		}
		if patch.Document.BackgroundMode != nil {
			document["backgroundMode"], _ = json.Marshal(*patch.Document.BackgroundMode)
		}
		if patch.Document.ShowImageInfo != nil {
			document["showImageInfo"], _ = json.Marshal(*patch.Document.ShowImageInfo)
		}
		if patch.Document.DirectorScenes != nil {
			document["directorScenes"] = append(json.RawMessage(nil), (*patch.Document.DirectorScenes)...)
		}
		if patch.Document.Viewport != nil {
			document["viewport"], _ = json.Marshal(*patch.Document.Viewport)
		}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return "", "", err
	}
	if len(encoded) > maxCanvasDocumentPayloadSize {
		return "", "", BadAuthRequest("画布数据不能超过 5MB")
	}
	return string(encoded), title, nil
}

func validCanvasViewportPatch(viewport CanvasViewportPatch) bool {
	finite := func(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
	return finite(viewport.X) && finite(viewport.Y) && finite(viewport.K) &&
		viewport.X >= -1_000_000 && viewport.X <= 1_000_000 &&
		viewport.Y >= -1_000_000 && viewport.Y <= 1_000_000 &&
		viewport.K >= 0.01 && viewport.K <= 16
}

func validateConnectionTypes(nodes []json.RawMessage, connections []json.RawMessage) error {
	nodeTypes := make(map[string]string, len(nodes))
	for _, raw := range nodes {
		var node struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &node); err != nil {
			return BadAuthRequest("节点数据格式无效")
		}
		nodeTypes[node.ID] = node.Type
	}
	for _, raw := range connections {
		var connection struct {
			FromNodeID string `json:"fromNodeId"`
			ToNodeID   string `json:"toNodeId"`
		}
		if err := json.Unmarshal(raw, &connection); err != nil {
			return BadAuthRequest("连线数据格式无效")
		}
		if nodeTypes[connection.FromNodeID] == "video" && (nodeTypes[connection.ToNodeID] == "image" || nodeTypes[connection.ToNodeID] == "audio") {
			return BadAuthRequest("视频节点的输出不能连接图片或音频节点")
		}
	}
	return nil
}

func applyEntityPatch(source json.RawMessage, upserts []json.RawMessage, deletes []string) ([]json.RawMessage, error) {
	current := []json.RawMessage{}
	if len(source) > 0 && string(source) != "null" {
		if err := json.Unmarshal(source, &current); err != nil {
			return nil, BadAuthRequest("画布实体列表格式无效")
		}
	}
	deleteSet := make(map[string]struct{}, len(deletes))
	for _, id := range deletes {
		deleteSet[id] = struct{}{}
	}
	upsertByID := make(map[string]json.RawMessage, len(upserts))
	for _, raw := range upserts {
		id, err := rawEntityID(raw)
		if err != nil {
			return nil, err
		}
		upsertByID[id] = append(json.RawMessage(nil), raw...)
	}
	result := make([]json.RawMessage, 0, len(current)+len(upserts))
	seen := map[string]struct{}{}
	for _, raw := range current {
		id, err := rawEntityID(raw)
		if err != nil {
			return nil, err
		}
		if _, deleted := deleteSet[id]; deleted {
			continue
		}
		if replacement, exists := upsertByID[id]; exists {
			result = append(result, replacement)
			seen[id] = struct{}{}
			continue
		}
		result = append(result, raw)
		seen[id] = struct{}{}
	}
	for _, raw := range upserts {
		id, _ := rawEntityID(raw)
		if _, exists := seen[id]; !exists {
			result = append(result, raw)
		}
	}
	return result, nil
}

func rawEntityID(raw json.RawMessage) (string, error) {
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil || strings.TrimSpace(identity.ID) == "" {
		return "", BadAuthRequest("画布实体缺少有效 ID")
	}
	return strings.TrimSpace(identity.ID), nil
}

func validateConnectionEndpoints(nodes []json.RawMessage, connections []json.RawMessage) error {
	nodeIDs := make(map[string]struct{}, len(nodes))
	for _, raw := range nodes {
		id, err := rawEntityID(raw)
		if err != nil {
			return err
		}
		nodeIDs[id] = struct{}{}
	}
	for _, raw := range connections {
		var connection struct {
			FromNodeID string `json:"fromNodeId"`
			ToNodeID   string `json:"toNodeId"`
		}
		if err := json.Unmarshal(raw, &connection); err != nil {
			return BadAuthRequest("画布连线格式无效")
		}
		if _, exists := nodeIDs[connection.FromNodeID]; !exists {
			return BadAuthRequest("画布连线的起点节点不存在")
		}
		if _, exists := nodeIDs[connection.ToNodeID]; !exists {
			return BadAuthRequest("画布连线的终点节点不存在")
		}
	}
	return nil
}
