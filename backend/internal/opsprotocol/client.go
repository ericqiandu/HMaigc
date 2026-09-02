package opsprotocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxControllerResponseBytes = 4 << 20

type Client interface {
	Overview(context.Context) (*Overview, error)
	Operations(context.Context, int) (*OperationPage, error)
	Operation(context.Context, string) (*Operation, error)
	OperationLogs(context.Context, string, uint64, int) (*OperationLogPage, error)
	Backups(context.Context, int) ([]Backup, error)
	StartOperation(context.Context, StartOperationRequest) (*Operation, error)
	CancelOperation(context.Context, string, CancelOperationRequest) (*Operation, error)
	RecoverOperation(context.Context, string, RecoverOperationRequest) (*Operation, error)
}

type SignedClient struct {
	httpClient *http.Client
	secret     []byte
}

type controllerEnvelope[T object] struct {
	Code int    `json:"code"`
	Data T      `json:"data"`
	Msg  string `json:"msg"`
}

type object interface {
	Overview | OperationPage | Operation | OperationLogPage | []Backup
}

type RemoteError struct {
	Status  int
	Message string
}

func (e *RemoteError) Error() string {
	return e.Message
}

func NewUnixClient(socketPath string, secret []byte) (*SignedClient, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, errors.New("运维控制器 socket 未配置")
	}
	if len(secret) < 32 {
		return nil, errors.New("运维控制器共享密钥长度不足")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     30 * time.Second,
	}
	return &SignedClient{
		httpClient: &http.Client{Transport: transport, Timeout: 15 * time.Second},
		secret:     append([]byte(nil), secret...),
	}, nil
}

func (c *SignedClient) Overview(ctx context.Context) (*Overview, error) {
	return doControllerRequest[Overview](ctx, c, http.MethodGet, "/v1/overview", nil)
}

func (c *SignedClient) Operations(ctx context.Context, limit int) (*OperationPage, error) {
	path := "/v1/operations?limit=" + strconv.Itoa(limit)
	return doControllerRequest[OperationPage](ctx, c, http.MethodGet, path, nil)
}

func (c *SignedClient) Operation(ctx context.Context, id string) (*Operation, error) {
	path := "/v1/operations/" + url.PathEscape(id)
	return doControllerRequest[Operation](ctx, c, http.MethodGet, path, nil)
}

func (c *SignedClient) OperationLogs(ctx context.Context, id string, after uint64, limit int) (*OperationLogPage, error) {
	path := "/v1/operations/" + url.PathEscape(id) + "/logs?after=" + strconv.FormatUint(after, 10) + "&limit=" + strconv.Itoa(limit)
	return doControllerRequest[OperationLogPage](ctx, c, http.MethodGet, path, nil)
}

func (c *SignedClient) Backups(ctx context.Context, limit int) ([]Backup, error) {
	path := "/v1/backups?limit=" + strconv.Itoa(limit)
	result, err := doControllerRequest[[]Backup](ctx, c, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return *result, nil
}

func (c *SignedClient) StartOperation(ctx context.Context, input StartOperationRequest) (*Operation, error) {
	return doControllerRequest[Operation](ctx, c, http.MethodPost, "/v1/operations", input)
}

func (c *SignedClient) CancelOperation(ctx context.Context, id string, input CancelOperationRequest) (*Operation, error) {
	path := "/v1/operations/" + url.PathEscape(id) + "/cancel"
	return doControllerRequest[Operation](ctx, c, http.MethodPost, path, input)
}

func (c *SignedClient) RecoverOperation(ctx context.Context, id string, input RecoverOperationRequest) (*Operation, error) {
	path := "/v1/operations/" + url.PathEscape(id) + "/recover"
	return doControllerRequest[Operation](ctx, c, http.MethodPost, path, input)
}

func doControllerRequest[T object](ctx context.Context, client *SignedClient, method string, path string, input interface{}) (*T, error) {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://hmaigc-ops"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce, err := newNonce()
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(HeaderTimestamp, timestamp)
	request.Header.Set(HeaderNonce, nonce)
	request.Header.Set(HeaderSignature, Signature(client.secret, method, request.URL.RequestURI(), timestamp, nonce, body))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("连接运维控制器失败: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxControllerResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > maxControllerResponseBytes {
		return nil, errors.New("运维控制器响应超过大小限制")
	}
	var envelope controllerEnvelope[T]
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, fmt.Errorf("运维控制器响应格式无效: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.Code != 0 {
		message := strings.TrimSpace(envelope.Msg)
		if message == "" {
			message = "运维控制器请求失败"
		}
		return nil, &RemoteError{Status: response.StatusCode, Message: message}
	}
	return &envelope.Data, nil
}

func newNonce() (string, error) {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer[:]), nil
}
