package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/service"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
)

type canvasCollaborationHub struct {
	svc      *service.Service
	mu       sync.RWMutex
	clients  map[string]map[*canvasRealtimeClient]struct{}
	useRedis bool
}

type canvasRealtimeClient struct {
	canvasID     string
	connectionID string
	send         chan []byte
	cancel       context.CancelFunc
}

type canvasRealtimeEnvelope struct {
	Type         string                            `json:"type"`
	CanvasID     string                            `json:"canvasId"`
	ConnectionID string                            `json:"connectionId,omitempty"`
	State        *service.CanvasCollaborationState `json:"state,omitempty"`
	Mutation     *service.CanvasMutationResult     `json:"mutation,omitempty"`
	Presence     *canvasPresence                   `json:"presence,omitempty"`
	Error        *canvasRealtimeError              `json:"error,omitempty"`
}

type canvasRealtimeError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

type canvasPresence struct {
	ConnectionID    string        `json:"connectionId"`
	UserID          string        `json:"userId"`
	DisplayName     string        `json:"displayName"`
	AvatarURL       string        `json:"avatarUrl,omitempty"`
	Cursor          *canvasCursor `json:"cursor,omitempty"`
	SelectedNodeIDs []string      `json:"selectedNodeIds,omitempty"`
	Active          bool          `json:"active"`
	UpdatedAt       time.Time     `json:"updatedAt"`
}

type canvasCursor struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type canvasClientPresence struct {
	Cursor          *canvasCursor `json:"cursor,omitempty"`
	SelectedNodeIDs []string      `json:"selectedNodeIds,omitempty"`
}

type canvasClientMessage struct {
	Type     string                         `json:"type"`
	Mutation *service.CanvasMutationRequest `json:"mutation,omitempty"`
	Presence *canvasClientPresence          `json:"presence,omitempty"`
}

func RegisterCanvasCollaborationRoutes(r *gin.RouterGroup, svc *service.Service) error {
	hub := &canvasCollaborationHub{svc: svc, clients: map[string]map[*canvasRealtimeClient]struct{}{}}
	_, useRedis, err := svc.SubscribeCanvasCollaborationEvents(context.Background(), hub.broadcast)
	if err != nil {
		return err
	}
	hub.useRedis = useRedis

	r.GET("/canvas-projects/:id/collaboration", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		state, err := svc.CanvasCollaboration(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, state)
	})

	r.PATCH("/canvas-projects/:id/collaboration", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var req service.ConfigureCanvasCollaborationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		state, err := svc.ConfigureCanvasCollaboration(user, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, state)
	})

	r.PUT("/canvas-projects/:id/collaborators/:userId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var req struct {
			Access string `json:"access"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		state, err := svc.UpdateCanvasCollaborator(user, c.Param("id"), service.UpdateCanvasCollaboratorRequest{
			UserID: c.Param("userId"),
			Access: model.CanvasAccessLevel(req.Access),
		})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, state)
	})

	r.DELETE("/canvas-projects/:id/collaborators/:userId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		state, err := svc.DeleteCanvasCollaborator(user, c.Param("id"), c.Param("userId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, state)
	})

	r.POST("/canvas-projects/:id/mutations", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		var req service.CanvasMutationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.CommitCanvasMutation(user, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		hub.emit(c.Request.Context(), c.Param("id"), canvasRealtimeEnvelope{
			Type: "mutation", CanvasID: c.Param("id"), Mutation: result,
		})
		ok(c, result)
	})

	r.GET("/canvas-projects/:id/resources/:resourceId/file", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		resource, err := svc.CanvasResource(user.ID, c.Param("id"), c.Param("resourceId"))
		if err != nil {
			fail(c, http.StatusNotFound, errors.New("画布资源不存在或无权访问"))
			return
		}
		etag := resourceResponseETag(resource)
		c.Header("Cache-Control", "private, no-cache")
		c.Header("ETag", etag)
		c.Header("Accept-Ranges", "bytes")
		c.Header("X-Content-Type-Options", "nosniff")
		if resource.Kind == "file" {
			c.Header("Content-Disposition", "attachment")
			c.Header("Content-Security-Policy", "sandbox")
		}
		if ifNoneMatch(c.GetHeader("If-None-Match"), etag) {
			c.Status(http.StatusNotModified)
			return
		}
		rangeHeader := c.GetHeader("Range")
		if ifRange := strings.TrimSpace(c.GetHeader("If-Range")); ifRange != "" && ifRange != etag {
			rangeHeader = ""
		}
		stream, err := svc.OpenCanvasResourceRange(user.ID, c.Param("id"), resource.ID, rangeHeader)
		if err != nil {
			failService(c, err)
			return
		}
		defer stream.Body.Close()
		mimeType := resource.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if resource.Provider == "local" {
			if seeker, ok := stream.Body.(io.ReadSeeker); ok {
				c.Header("Content-Type", mimeType)
				http.ServeContent(c.Writer, c.Request, resource.ID, resource.UpdatedAt, seeker)
				return
			}
		}
		if stream.ContentRange != "" {
			c.Header("Content-Range", stream.ContentRange)
		}
		if stream.AcceptRanges != "" {
			c.Header("Accept-Ranges", stream.AcceptRanges)
		}
		c.DataFromReader(stream.StatusCode, stream.ContentLength, mimeType, stream.Body, nil)
	})

	r.GET("/canvas-projects/:id/collaboration/socket", func(c *gin.Context) {
		hub.serveSocket(c)
	})
	return nil
}

func (h *canvasCollaborationHub) serveSocket(c *gin.Context) {
	user, err := currentUser(c, h.svc)
	if err != nil {
		failService(c, err)
		return
	}
	canvasID := strings.TrimSpace(c.Param("id"))
	state, err := h.svc.CanvasCollaboration(user, canvasID)
	if err != nil {
		failService(c, err)
		return
	}
	if state.Access.TeamID == "" {
		fail(c, http.StatusBadRequest, service.BadAuthRequest("个人画布不使用多人实时协作"))
		return
	}
	publicUser, err := h.svc.PublicAuthUser(user)
	if err != nil {
		failService(c, err)
		return
	}
	connectionID, err := secureConnectionID()
	if err != nil {
		failService(c, err)
		return
	}
	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		log.Printf("canvas collaboration websocket accept failed canvas=%s user=%s: %v", canvasID, user.ID, err)
		return
	}
	conn.SetReadLimit(2 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	client := &canvasRealtimeClient{canvasID: canvasID, connectionID: connectionID, send: make(chan []byte, 64), cancel: cancel}
	h.register(client)

	defer cancel()
	defer conn.Close(websocket.StatusNormalClosure, "")
	defer h.unregister(client)

	snapshot := canvasRealtimeEnvelope{
		Type: "snapshot", CanvasID: canvasID, ConnectionID: connectionID, State: state,
	}
	if !h.enqueue(client, snapshot) {
		return
	}
	joined := canvasPresence{
		ConnectionID: connectionID, UserID: user.ID,
		DisplayName: firstNonEmptyHandler(publicUser.DisplayName, publicUser.Username),
		AvatarURL:   publicUser.AvatarURL, Active: true, UpdatedAt: time.Now(),
	}
	h.emit(ctx, canvasID, canvasRealtimeEnvelope{
		Type: "presence", CanvasID: canvasID, ConnectionID: connectionID, Presence: &joined,
	})

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writeCanvasRealtime(ctx, conn, client.send)
	}()
	readDone := make(chan error, 1)
	go func() {
		readDone <- h.readCanvasRealtime(ctx, conn, client, user, publicUser.AvatarURL)
	}()
	select {
	case err = <-writeDone:
	case err = <-readDone:
	}
	if err != nil && websocket.CloseStatus(err) == -1 && !errors.Is(err, context.Canceled) {
		log.Printf("canvas collaboration websocket closed canvas=%s user=%s: %v", canvasID, user.ID, err)
	}
	left := joined
	left.Active = false
	left.UpdatedAt = time.Now()
	h.emit(context.Background(), canvasID, canvasRealtimeEnvelope{
		Type: "presence", CanvasID: canvasID, ConnectionID: connectionID, Presence: &left,
	})
}

func (h *canvasCollaborationHub) readCanvasRealtime(
	ctx context.Context,
	conn *websocket.Conn,
	client *canvasRealtimeClient,
	user *model.User,
	avatarURL string,
) error {
	lastPresenceAt := time.Time{}
	for {
		var message canvasClientMessage
		if err := wsjson.Read(ctx, conn, &message); err != nil {
			return err
		}
		switch message.Type {
		case "mutation":
			if message.Mutation == nil {
				h.sendError(client, http.StatusBadRequest, "协作变更内容不能为空")
				continue
			}
			result, err := h.svc.CommitCanvasMutation(user, client.canvasID, *message.Mutation)
			if err != nil {
				h.sendServiceError(client, err)
				continue
			}
			h.emit(ctx, client.canvasID, canvasRealtimeEnvelope{
				Type: "mutation", CanvasID: client.canvasID, ConnectionID: client.connectionID, Mutation: result,
			})
		case "presence":
			if message.Presence == nil || time.Since(lastPresenceAt) < 30*time.Millisecond {
				continue
			}
			if err := validateCanvasPresence(*message.Presence); err != nil {
				h.sendServiceError(client, err)
				continue
			}
			lastPresenceAt = time.Now()
			presence := canvasPresence{
				ConnectionID: client.connectionID, UserID: user.ID,
				DisplayName: firstNonEmptyHandler(user.DisplayName, user.Username),
				AvatarURL:   avatarURL, Cursor: message.Presence.Cursor,
				SelectedNodeIDs: message.Presence.SelectedNodeIDs,
				Active:          true, UpdatedAt: lastPresenceAt,
			}
			h.emit(ctx, client.canvasID, canvasRealtimeEnvelope{
				Type: "presence", CanvasID: client.canvasID, ConnectionID: client.connectionID, Presence: &presence,
			})
		default:
			h.sendError(client, http.StatusBadRequest, "不支持的协作消息类型")
		}
	}
}

func writeCanvasRealtime(ctx context.Context, conn *websocket.Conn, send <-chan []byte) error {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case payload, open := <-send:
			if !open {
				return nil
			}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, payload)
			cancel()
			if err != nil {
				return err
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

func (h *canvasCollaborationHub) register(client *canvasRealtimeClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.clients[client.canvasID]
	if room == nil {
		room = map[*canvasRealtimeClient]struct{}{}
		h.clients[client.canvasID] = room
	}
	room[client] = struct{}{}
}

func (h *canvasCollaborationHub) unregister(client *canvasRealtimeClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.clients[client.canvasID]
	if room == nil {
		return
	}
	if _, exists := room[client]; !exists {
		return
	}
	delete(room, client)
	client.cancel()
	if len(room) == 0 {
		delete(h.clients, client.canvasID)
	}
}

func (h *canvasCollaborationHub) emit(ctx context.Context, canvasID string, envelope canvasRealtimeEnvelope) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("marshal canvas collaboration event failed canvas=%s: %v", canvasID, err)
		return
	}
	if h.useRedis {
		published, publishErr := h.svc.PublishCanvasCollaborationEvent(ctx, canvasID, payload)
		if publishErr == nil && published {
			return
		}
		log.Printf("publish canvas collaboration event failed canvas=%s: %v", canvasID, publishErr)
	}
	h.broadcast(canvasID, payload)
}

func (h *canvasCollaborationHub) broadcast(canvasID string, payload []byte) {
	h.mu.RLock()
	room := h.clients[canvasID]
	clients := make([]*canvasRealtimeClient, 0, len(room))
	for client := range room {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		select {
		case client.send <- append([]byte(nil), payload...):
		default:
			// 慢连接不能阻塞整个房间；关闭队列使其重新连接并从权威快照恢复。
			h.unregister(client)
		}
	}
}

func (h *canvasCollaborationHub) enqueue(client *canvasRealtimeClient, envelope canvasRealtimeEnvelope) bool {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return false
	}
	select {
	case client.send <- payload:
		return true
	default:
		return false
	}
}

func (h *canvasCollaborationHub) sendServiceError(client *canvasRealtimeClient, err error) {
	var authErr *service.AuthError
	if errors.As(err, &authErr) {
		h.sendError(client, authErr.Status, authErr.Message)
		return
	}
	h.sendError(client, http.StatusInternalServerError, "画布协作操作失败")
	log.Printf("canvas collaboration operation failed canvas=%s connection=%s: %v", client.canvasID, client.connectionID, err)
}

func (h *canvasCollaborationHub) sendError(client *canvasRealtimeClient, status int, message string) {
	h.enqueue(client, canvasRealtimeEnvelope{
		Type: "error", CanvasID: client.canvasID, ConnectionID: client.connectionID,
		Error: &canvasRealtimeError{Status: status, Message: message},
	})
}

func validateCanvasPresence(presence canvasClientPresence) error {
	if len(presence.SelectedNodeIDs) > 100 {
		return service.BadAuthRequest("协作选中节点不能超过 100 个")
	}
	seen := map[string]struct{}{}
	for _, rawID := range presence.SelectedNodeIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || len(id) > 120 {
			return service.BadAuthRequest("协作选中节点 ID 无效")
		}
		if _, exists := seen[id]; exists {
			return service.BadAuthRequest("协作选中节点 ID 重复")
		}
		seen[id] = struct{}{}
	}
	if presence.Cursor != nil {
		if math.IsNaN(presence.Cursor.X) || math.IsNaN(presence.Cursor.Y) ||
			math.IsInf(presence.Cursor.X, 0) || math.IsInf(presence.Cursor.Y, 0) ||
			math.Abs(presence.Cursor.X) > 1e7 || math.Abs(presence.Cursor.Y) > 1e7 {
			return service.BadAuthRequest("协作光标坐标无效")
		}
	}
	return nil
}

func secureConnectionID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func firstNonEmptyHandler(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "团队成员"
}
