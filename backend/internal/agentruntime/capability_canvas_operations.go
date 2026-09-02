package agentruntime

import (
	"bytes"
	"encoding/json"
	"math"
)

type CanvasOperationType string

type CanvasNodeType string

type CanvasNodeStatus string

const (
	CanvasOperationAddNode           CanvasOperationType = "add_node"
	CanvasOperationUpdateNode        CanvasOperationType = "update_node"
	CanvasOperationDeleteNode        CanvasOperationType = "delete_node"
	CanvasOperationConnectNodes      CanvasOperationType = "connect_nodes"
	CanvasOperationDeleteConnections CanvasOperationType = "delete_connections"
	CanvasOperationSetViewport       CanvasOperationType = "set_viewport"
	CanvasOperationSelectNodes       CanvasOperationType = "select_nodes"
)

const (
	CanvasNodeImage  CanvasNodeType = "image"
	CanvasNodeText   CanvasNodeType = "text"
	CanvasNodeScript CanvasNodeType = "script"
	CanvasNodeSkill  CanvasNodeType = "skill"
	CanvasNodeConfig CanvasNodeType = "config"
	CanvasNodeVideo  CanvasNodeType = "video"
	CanvasNodeAudio  CanvasNodeType = "audio"
	CanvasNodeFrame  CanvasNodeType = "frame"
)

const (
	CanvasNodeIdle    CanvasNodeStatus = "idle"
	CanvasNodeSuccess CanvasNodeStatus = "success"
	CanvasNodeLoading CanvasNodeStatus = "loading"
	CanvasNodeError   CanvasNodeStatus = "error"
)

type CanvasPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type CanvasViewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

type CanvasNodeInput struct {
	ID       string          `json:"id"`
	Type     CanvasNodeType  `json:"type"`
	Title    string          `json:"title"`
	Position CanvasPoint     `json:"position"`
	Width    float64         `json:"width"`
	Height   float64         `json:"height"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type CanvasConnectionInput struct {
	ID           string `json:"id"`
	FromNodeID   string `json:"fromNodeId"`
	ToNodeID     string `json:"toNodeId"`
	FromHandleID string `json:"fromHandleId,omitempty"`
	ToHandleID   string `json:"toHandleId,omitempty"`
}

type CanvasOperation struct {
	OperationID   string                 `json:"operationId"`
	Type          CanvasOperationType    `json:"type"`
	Node          *CanvasNodeInput       `json:"node,omitempty"`
	NodeID        string                 `json:"nodeId,omitempty"`
	Patch         json.RawMessage        `json:"patch,omitempty"`
	Connection    *CanvasConnectionInput `json:"connection,omitempty"`
	ConnectionIDs []string               `json:"connectionIds,omitempty"`
	NodeIDs       []string               `json:"nodeIds,omitempty"`
	Viewport      *CanvasViewport        `json:"viewport,omitempty"`
}

func (operation CanvasOperation) MarshalJSON() ([]byte, error) {
	switch operation.Type {
	case CanvasOperationAddNode:
		return json.Marshal(struct {
			OperationID string              `json:"operationId"`
			Type        CanvasOperationType `json:"type"`
			Node        *CanvasNodeInput    `json:"node"`
		}{OperationID: operation.OperationID, Type: operation.Type, Node: operation.Node})
	case CanvasOperationUpdateNode:
		return json.Marshal(struct {
			OperationID string              `json:"operationId"`
			Type        CanvasOperationType `json:"type"`
			NodeID      string              `json:"nodeId"`
			Patch       json.RawMessage     `json:"patch"`
		}{OperationID: operation.OperationID, Type: operation.Type, NodeID: operation.NodeID, Patch: operation.Patch})
	case CanvasOperationDeleteNode:
		return json.Marshal(struct {
			OperationID string              `json:"operationId"`
			Type        CanvasOperationType `json:"type"`
			NodeID      string              `json:"nodeId"`
		}{OperationID: operation.OperationID, Type: operation.Type, NodeID: operation.NodeID})
	case CanvasOperationConnectNodes:
		return json.Marshal(struct {
			OperationID string                 `json:"operationId"`
			Type        CanvasOperationType    `json:"type"`
			Connection  *CanvasConnectionInput `json:"connection"`
		}{OperationID: operation.OperationID, Type: operation.Type, Connection: operation.Connection})
	case CanvasOperationDeleteConnections:
		return json.Marshal(struct {
			OperationID   string              `json:"operationId"`
			Type          CanvasOperationType `json:"type"`
			ConnectionIDs []string            `json:"connectionIds"`
		}{OperationID: operation.OperationID, Type: operation.Type, ConnectionIDs: operation.ConnectionIDs})
	case CanvasOperationSetViewport:
		return json.Marshal(struct {
			OperationID string              `json:"operationId"`
			Type        CanvasOperationType `json:"type"`
			Viewport    *CanvasViewport     `json:"viewport"`
		}{OperationID: operation.OperationID, Type: operation.Type, Viewport: operation.Viewport})
	case CanvasOperationSelectNodes:
		return json.Marshal(struct {
			OperationID string              `json:"operationId"`
			Type        CanvasOperationType `json:"type"`
			NodeIDs     []string            `json:"nodeIds"`
		}{OperationID: operation.OperationID, Type: operation.Type, NodeIDs: operation.NodeIDs})
	default:
		return nil, errCapabilityArgumentsInvalid
	}
}

func decodeCanvasOperation(payload json.RawMessage) (CanvasOperation, error) {
	var discriminator struct {
		Type CanvasOperationType `json:"type"`
	}
	if err := json.Unmarshal(payload, &discriminator); err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	switch discriminator.Type {
	case CanvasOperationAddNode:
		return decodeCanvasAddNodeOperation(payload)
	case CanvasOperationUpdateNode:
		return decodeCanvasUpdateNodeOperation(payload)
	case CanvasOperationDeleteNode:
		return decodeCanvasDeleteNodeOperation(payload)
	case CanvasOperationConnectNodes:
		return decodeCanvasConnectNodesOperation(payload)
	case CanvasOperationDeleteConnections:
		return decodeCanvasDeleteConnectionsOperation(payload)
	case CanvasOperationSetViewport:
		return decodeCanvasSetViewportOperation(payload)
	case CanvasOperationSelectNodes:
		return decodeCanvasSelectNodesOperation(payload)
	default:
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
}

func decodeCanvasAddNodeOperation(payload json.RawMessage) (CanvasOperation, error) {
	var wire struct {
		OperationID string              `json:"operationId"`
		Type        CanvasOperationType `json:"type"`
		Node        *struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Title    string `json:"title"`
			Position *struct {
				X *float64 `json:"x"`
				Y *float64 `json:"y"`
			} `json:"position"`
			Width    *float64        `json:"width"`
			Height   *float64        `json:"height"`
			Metadata json.RawMessage `json:"metadata,omitempty"`
		} `json:"node"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Type != CanvasOperationAddNode || wire.Node == nil || wire.Node.Position == nil || wire.Node.Position.X == nil || wire.Node.Position.Y == nil || wire.Node.Width == nil || wire.Node.Height == nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, nodeID, err := normalizeOperationAndNodeID(wire.OperationID, wire.Node.ID)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	nodeTypeText, err := normalizeText(wire.Node.Type, 64)
	nodeType := CanvasNodeType(nodeTypeText)
	if err != nil || !validCanvasNodeType(nodeType) {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	title, err := normalizeText(wire.Node.Title, capabilityDisplayNameLimit)
	if err != nil || !validCoordinate(*wire.Node.Position.X) || !validCoordinate(*wire.Node.Position.Y) || !validDimension(*wire.Node.Width) || !validDimension(*wire.Node.Height) {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	metadata, err := normalizeJSONObject(wire.Node.Metadata, capabilityParametersLimit, true)
	if err != nil || !validCanvasNodeMetadata(metadata) {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	node := &CanvasNodeInput{ID: nodeID, Type: nodeType, Title: title, Position: CanvasPoint{X: *wire.Node.Position.X, Y: *wire.Node.Position.Y}, Width: *wire.Node.Width, Height: *wire.Node.Height, Metadata: metadata}
	return CanvasOperation{OperationID: operationID, Type: wire.Type, Node: node}, nil
}

func decodeCanvasUpdateNodeOperation(payload json.RawMessage) (CanvasOperation, error) {
	var wire struct {
		OperationID string              `json:"operationId"`
		Type        CanvasOperationType `json:"type"`
		NodeID      string              `json:"nodeId"`
		Patch       json.RawMessage     `json:"patch"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Type != CanvasOperationUpdateNode {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, nodeID, err := normalizeOperationAndNodeID(wire.OperationID, wire.NodeID)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	patch, err := normalizeJSONObject(wire.Patch, capabilityParametersLimit, false)
	if err != nil || bytes.Equal(patch, []byte("{}")) || !validCanvasNodePatch(patch) {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	return CanvasOperation{OperationID: operationID, Type: wire.Type, NodeID: nodeID, Patch: patch}, nil
}

func decodeCanvasDeleteNodeOperation(payload json.RawMessage) (CanvasOperation, error) {
	var wire struct {
		OperationID string              `json:"operationId"`
		Type        CanvasOperationType `json:"type"`
		NodeID      string              `json:"nodeId"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Type != CanvasOperationDeleteNode {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, nodeID, err := normalizeOperationAndNodeID(wire.OperationID, wire.NodeID)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	return CanvasOperation{OperationID: operationID, Type: wire.Type, NodeID: nodeID}, nil
}

func decodeCanvasConnectNodesOperation(payload json.RawMessage) (CanvasOperation, error) {
	var wire struct {
		OperationID string                 `json:"operationId"`
		Type        CanvasOperationType    `json:"type"`
		Connection  *CanvasConnectionInput `json:"connection"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Type != CanvasOperationConnectNodes || wire.Connection == nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, err := normalizeIdentifier(wire.OperationID, capabilityIdentifierLimit)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	connectionID, err := normalizeIdentifier(wire.Connection.ID, capabilityIdentifierLimit)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	fromNodeID, err := normalizeIdentifier(wire.Connection.FromNodeID, capabilityIdentifierLimit)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	toNodeID, err := normalizeIdentifier(wire.Connection.ToNodeID, capabilityIdentifierLimit)
	if err != nil || fromNodeID == toNodeID {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	fromHandleID, err := normalizeOptionalIdentifier(wire.Connection.FromHandleID, capabilityIdentifierLimit)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	targetHandleID, err := normalizeOptionalIdentifier(wire.Connection.ToHandleID, capabilityIdentifierLimit)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	connection := &CanvasConnectionInput{ID: connectionID, FromNodeID: fromNodeID, ToNodeID: toNodeID, FromHandleID: fromHandleID, ToHandleID: targetHandleID}
	return CanvasOperation{OperationID: operationID, Type: wire.Type, Connection: connection}, nil
}

func decodeCanvasDeleteConnectionsOperation(payload json.RawMessage) (CanvasOperation, error) {
	var wire struct {
		OperationID   string              `json:"operationId"`
		Type          CanvasOperationType `json:"type"`
		ConnectionIDs []string            `json:"connectionIds"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Type != CanvasOperationDeleteConnections || wire.ConnectionIDs == nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, err := normalizeIdentifier(wire.OperationID, capabilityIdentifierLimit)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	connectionIDs, err := normalizeIdentifiers(wire.ConnectionIDs, capabilityOperationLimit, false)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	return CanvasOperation{OperationID: operationID, Type: wire.Type, ConnectionIDs: connectionIDs}, nil
}

func decodeCanvasSetViewportOperation(payload json.RawMessage) (CanvasOperation, error) {
	var wire struct {
		OperationID string              `json:"operationId"`
		Type        CanvasOperationType `json:"type"`
		Viewport    *struct {
			X    *float64 `json:"x"`
			Y    *float64 `json:"y"`
			Zoom *float64 `json:"zoom"`
		} `json:"viewport"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Type != CanvasOperationSetViewport || wire.Viewport == nil || wire.Viewport.X == nil || wire.Viewport.Y == nil || wire.Viewport.Zoom == nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, err := normalizeIdentifier(wire.OperationID, capabilityIdentifierLimit)
	if err != nil || !validCoordinate(*wire.Viewport.X) || !validCoordinate(*wire.Viewport.Y) || !validZoom(*wire.Viewport.Zoom) {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	viewport := &CanvasViewport{X: *wire.Viewport.X, Y: *wire.Viewport.Y, Zoom: *wire.Viewport.Zoom}
	return CanvasOperation{OperationID: operationID, Type: wire.Type, Viewport: viewport}, nil
}

func decodeCanvasSelectNodesOperation(payload json.RawMessage) (CanvasOperation, error) {
	var wire struct {
		OperationID string              `json:"operationId"`
		Type        CanvasOperationType `json:"type"`
		NodeIDs     []string            `json:"nodeIds"`
	}
	if decodeStrictCapabilityJSON(payload, func(decoder *json.Decoder) error {
		return decoder.Decode(&wire)
	}, capabilityPayloadLimit) != nil || wire.Type != CanvasOperationSelectNodes || wire.NodeIDs == nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	operationID, err := normalizeIdentifier(wire.OperationID, capabilityIdentifierLimit)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	nodeIDs, err := normalizeIdentifiers(wire.NodeIDs, capabilityResourceLimit, true)
	if err != nil {
		return CanvasOperation{}, errCapabilityArgumentsInvalid
	}
	return CanvasOperation{OperationID: operationID, Type: wire.Type, NodeIDs: nodeIDs}, nil
}

func validCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= -1_000_000 && value <= 1_000_000
}

func validDimension(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 1 && value <= 20_000
}

func validZoom(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0.01 && value <= 16
}

func validViewport(viewport CanvasViewport) bool {
	return validCoordinate(viewport.X) && validCoordinate(viewport.Y) && validZoom(viewport.Zoom)
}

func validCanvasNodeType(nodeType CanvasNodeType) bool {
	switch nodeType {
	case CanvasNodeImage, CanvasNodeText, CanvasNodeScript, CanvasNodeSkill, CanvasNodeConfig, CanvasNodeVideo, CanvasNodeAudio, CanvasNodeFrame:
		return true
	default:
		return false
	}
}

func validCanvasNodeStatus(status CanvasNodeStatus) bool {
	switch status {
	case CanvasNodeIdle, CanvasNodeSuccess, CanvasNodeLoading, CanvasNodeError:
		return true
	default:
		return false
	}
}

func validCanvasNodeMetadata(metadata json.RawMessage) bool {
	var values map[string]json.RawMessage
	if json.Unmarshal(metadata, &values) != nil {
		return false
	}
	raw, found := values["status"]
	if !found {
		return true
	}
	var status CanvasNodeStatus
	return json.Unmarshal(raw, &status) == nil && validCanvasNodeStatus(status)
}

func validCanvasNodePatch(patch json.RawMessage) bool {
	var values map[string]json.RawMessage
	if json.Unmarshal(patch, &values) != nil {
		return false
	}
	if raw, found := values["type"]; found {
		var nodeType CanvasNodeType
		if json.Unmarshal(raw, &nodeType) != nil || !validCanvasNodeType(nodeType) {
			return false
		}
	}
	if raw, found := values["metadata"]; found && !validCanvasNodeMetadata(raw) {
		return false
	}
	return true
}
