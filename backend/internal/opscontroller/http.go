package opscontroller

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/opsprotocol"

	"gorm.io/gorm"
)

const maxControllerRequestBytes = 64 << 10

type HTTPServer struct {
	controller *Controller
	secret     []byte
	nonces     *nonceRegistry
}

type nonceRegistry struct {
	mu     sync.Mutex
	values map[string]time.Time
}

func NewHTTPHandler(controller *Controller, secret []byte) (http.Handler, error) {
	if controller == nil {
		return nil, errors.New("运维控制器不能为空")
	}
	if len(secret) < 32 {
		return nil, errors.New("运维控制器共享密钥长度不足")
	}
	server := &HTTPServer{
		controller: controller,
		secret:     append([]byte(nil), secret...),
		nonces:     &nonceRegistry{values: make(map[string]time.Time)},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", server.health)
	mux.HandleFunc("GET /v1/overview", server.overview)
	mux.HandleFunc("GET /v1/backups", server.backups)
	mux.HandleFunc("GET /v1/operations", server.operations)
	mux.HandleFunc("POST /v1/operations", server.startOperation)
	mux.HandleFunc("GET /v1/operations/{id}", server.operation)
	mux.HandleFunc("GET /v1/operations/{id}/logs", server.operationLogs)
	mux.HandleFunc("POST /v1/operations/{id}/cancel", server.cancelOperation)
	mux.HandleFunc("POST /v1/operations/{id}/recover", server.recoverOperation)
	return server.authenticate(mux), nil
}

func (s *HTTPServer) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, maxControllerRequestBytes)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "运维请求体无效")
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		timestamp := request.Header.Get(opsprotocol.HeaderTimestamp)
		nonce := request.Header.Get(opsprotocol.HeaderNonce)
		signature := request.Header.Get(opsprotocol.HeaderSignature)
		now := time.Now()
		if err := opsprotocol.VerifySignature(s.secret, request.Method, request.URL.RequestURI(), timestamp, nonce, body, signature, now, time.Minute); err != nil {
			writeError(writer, http.StatusUnauthorized, err.Error())
			return
		}
		if !s.nonces.use(nonce, now.Add(2*time.Minute), now) {
			writeError(writer, http.StatusConflict, "运维请求 nonce 已使用")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (r *nonceRegistry) use(nonce string, expiresAt time.Time, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, expiry := range r.values {
		if !expiry.After(now) {
			delete(r.values, key)
		}
	}
	if _, exists := r.values[nonce]; exists {
		return false
	}
	r.values[nonce] = expiresAt
	return true
}

func (s *HTTPServer) health(writer http.ResponseWriter, request *http.Request) {
	overview, err := s.controller.Overview(request.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeOK(writer, overview.Controller)
}

func (s *HTTPServer) overview(writer http.ResponseWriter, request *http.Request) {
	result, err := s.controller.Overview(request.Context())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(writer, result)
}

func (s *HTTPServer) backups(writer http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	result, err := s.controller.Backups(limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(writer, result)
}

func (s *HTTPServer) operations(writer http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	result, err := s.controller.Operations(limit)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(writer, result)
}

func (s *HTTPServer) startOperation(writer http.ResponseWriter, request *http.Request) {
	var input opsprotocol.StartOperationRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "运维请求格式无效")
		return
	}
	result, err := s.controller.StartOperation(input)
	if err != nil {
		var requestError *RequestError
		switch {
		case errors.Is(err, ErrOperationActive), errors.Is(err, ErrIdempotencyConflict):
			writeError(writer, http.StatusConflict, err.Error())
		case errors.As(err, &requestError):
			writeError(writer, http.StatusBadRequest, requestError.Error())
		default:
			writeError(writer, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeOK(writer, result)
}

func (s *HTTPServer) operation(writer http.ResponseWriter, request *http.Request) {
	result, err := s.controller.Operation(request.PathValue("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(writer, http.StatusNotFound, "运维任务不存在")
			return
		}
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(writer, result)
}

func (s *HTTPServer) operationLogs(writer http.ResponseWriter, request *http.Request) {
	after, _ := strconv.ParseUint(request.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	result, err := s.controller.OperationLogs(request.PathValue("id"), after, limit)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(writer, http.StatusNotFound, "运维任务不存在")
			return
		}
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(writer, result)
}

func (s *HTTPServer) cancelOperation(writer http.ResponseWriter, request *http.Request) {
	var input opsprotocol.CancelOperationRequest
	if !decodeControlRequest(writer, request, &input) {
		return
	}
	result, err := s.controller.CancelOperation(request.PathValue("id"), input)
	if err != nil {
		writeControlError(writer, err)
		return
	}
	writeOK(writer, result)
}

func (s *HTTPServer) recoverOperation(writer http.ResponseWriter, request *http.Request) {
	var input opsprotocol.RecoverOperationRequest
	if !decodeControlRequest(writer, request, &input) {
		return
	}
	result, err := s.controller.RecoverOperation(request.Context(), request.PathValue("id"), input)
	if err != nil {
		writeControlError(writer, err)
		return
	}
	writeOK(writer, result)
}

func decodeControlRequest(writer http.ResponseWriter, request *http.Request, destination interface{}) bool {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(writer, http.StatusBadRequest, "运维控制请求格式无效")
		return false
	}
	return true
}

func writeControlError(writer http.ResponseWriter, err error) {
	var requestError *RequestError
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		writeError(writer, http.StatusNotFound, "运维任务不存在")
	case errors.Is(err, ErrIdempotencyConflict),
		errors.Is(err, ErrCancellationNotAllowed),
		errors.Is(err, ErrRecoveryNotAllowed),
		errors.Is(err, opsprotocol.ErrInvalidStatusTransition):
		writeError(writer, http.StatusConflict, err.Error())
	case errors.As(err, &requestError):
		writeError(writer, http.StatusBadRequest, requestError.Error())
	default:
		writeError(writer, http.StatusInternalServerError, err.Error())
	}
}

func writeOK(writer http.ResponseWriter, data interface{}) {
	writeJSON(writer, http.StatusOK, map[string]interface{}{"code": 0, "data": data, "msg": "ok"})
}

func writeError(writer http.ResponseWriter, status int, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		message = http.StatusText(status)
	}
	writeJSON(writer, status, map[string]interface{}{"code": status, "data": nil, "msg": message})
}

func writeJSON(writer http.ResponseWriter, status int, value interface{}) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
