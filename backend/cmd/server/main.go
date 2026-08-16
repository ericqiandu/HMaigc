package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/buildinfo"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/handler"
	"infinite-canvas/backend/internal/opsprotocol"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func main() {
	dataDir := env("CANVAS_BACKEND_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(database.Config{
		Driver:  env("CANVAS_DATABASE_DRIVER", "sqlite"),
		DSN:     os.Getenv("DATABASE_URL"),
		DataDir: dataDir,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := database.ConfigurePool(db); err != nil {
		log.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()
	if err := database.MigrateSchema(db); err != nil {
		log.Fatal(err)
	}

	repo := repository.New(db)
	svc := service.New(repo, dataDir)
	if err := runStartupRuntimeGate(svc, func() error {
		return initializeAndServe(sqlDB, svc)
	}); err != nil {
		log.Fatal(err)
	}
}

type startupRuntimeValidator interface {
	MigrateKuaiziAccountCredential() error
	ValidateStartupRuntime() error
}

// runStartupRuntimeGate 保证支付配置与 provider 密钥根在 worker、readiness 和 listener 之前完成强校验。
func runStartupRuntimeGate(validator startupRuntimeValidator, afterValidation func() error) error {
	if err := validator.MigrateKuaiziAccountCredential(); err != nil {
		return err
	}
	if err := validator.ValidateStartupRuntime(); err != nil {
		return err
	}
	return afterValidation()
}

func initializeAndServe(sqlDB *sql.DB, svc *service.Service) error {
	if err := svc.RemoveLegacyChannelModelIcons(); err != nil {
		return err
	}
	if err := configureOperationsClient(svc); err != nil {
		return err
	}
	if err := svc.ValidateRuntime(); err != nil {
		return err
	}
	if err := svc.EnsureSystemChannelModels(); err != nil {
		return err
	}
	if err := svc.EnsureDefaultChannelVoices(); err != nil {
		return err
	}
	if err := svc.EnsureDefaultStoryboardPromptTemplate(); err != nil {
		return err
	}
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		return err
	}
	if err := svc.EnsureDefaultCreditTopupProducts(); err != nil {
		return fmt.Errorf("initialize credit store: %w", err)
	}
	if err := svc.EnsureBuiltinProjectWorkflowTemplate(); err != nil {
		return err
	}
	if summary, err := svc.MigrateLegacyStorage(); err != nil {
		log.Printf("storage migration skipped after error: %v", err)
	} else if summary.Backup != "" {
		log.Printf("storage migration completed: tasks=%d assets=%d projects=%d backup=%s", summary.Tasks, summary.Assets, summary.Projects, summary.Backup)
	}
	svc.StartWorker()
	svc.StartStorageMigrationWorker()

	r := gin.New()
	r.Use(handler.PaymentCapabilityHeaders(), requestLogger(gin.DefaultWriter), requestRecovery(gin.DefaultErrorWriter), cors())
	handler.ConfigureRuntime(svc)
	api := r.Group("/api")
	api.GET("/health", healthHandler(sqlDB, svc))
	handler.RegisterOAuthCallbackRoutes(r, svc)
	handler.RegisterAuthRoutes(api, svc)
	handler.RegisterAdminRoutes(api, svc)
	handler.RegisterAdminReleaseRoutes(api, svc, env("CANVAS_CHANGELOG_PATH", "../CHANGELOG.md"))
	handler.RegisterAdminOperationsRoutes(api, svc)
	handler.RegisterAdminStorageMigrationRoutes(api, svc)
	handler.RegisterAdminAnalyticsRoutes(api, svc)
	handler.RegisterAgentModelSettingRoutes(api, svc)
	handler.RegisterProviderAccountRoutes(api, svc)
	handler.RegisterAnnouncementRoutes(api, svc)
	handler.RegisterFinanceRoutes(api, svc)
	handler.RegisterCreditStoreRoutes(api, svc)
	handler.RegisterMembershipRoutes(api, svc)
	handler.RegisterReferralRoutes(api, svc)
	handler.RegisterTeamRoutes(api, svc)
	handler.RegisterPaymentRoutes(api, svc)
	handler.RegisterSiteSettingRoutes(api, svc)
	handler.RegisterWatermarkPolicyRoutes(api, svc)
	// 登录态模型目录代理：避免浏览器直连各上游时分别处理 CORS。
	handler.RegisterChannelModelRoutes(api, svc)
	handler.RegisterSystemProxyRoutes(api, svc)
	handler.RegisterCustomRelayRoutes(api, svc)
	handler.RegisterAgentRuntimeRoutes(api, svc)
	handler.RegisterTaskRoutes(api, svc)
	handler.RegisterSessionRoutes(api, svc)
	handler.RegisterSkillRoutes(api, svc)
	handler.RegisterUserDataRoutes(api, svc)
	handler.RegisterProjectRoutes(api, svc)
	handler.RegisterCanvasShareRoutes(api, svc)
	if err := handler.RegisterCanvasCollaborationRoutes(api, svc); err != nil {
		return err
	}

	addr := env("CANVAS_BACKEND_ADDR", ":8080")
	log.Printf("HMaigc backend listening on %s", addr)
	return runHTTPServer(newHTTPServer(addr, r))
}

func configureOperationsClient(svc *service.Service) error {
	socketPath := strings.TrimSpace(os.Getenv("HMAIGC_OPS_SOCKET"))
	secretPath := strings.TrimSpace(os.Getenv("HMAIGC_OPS_SHARED_SECRET_FILE"))
	if socketPath == "" && secretPath == "" {
		return nil
	}
	if socketPath == "" || secretPath == "" {
		return errors.New("HMAIGC_OPS_SOCKET 与 HMAIGC_OPS_SHARED_SECRET_FILE 必须同时配置")
	}
	secret, err := os.ReadFile(secretPath)
	if err != nil {
		return fmt.Errorf("读取运维控制器共享密钥失败: %w", err)
	}
	client, err := opsprotocol.NewUnixClient(socketPath, []byte(strings.TrimSpace(string(secret))))
	if err != nil {
		return err
	}
	svc.ConfigureOperationsClient(client)
	return nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Minute,
		WriteTimeout:      65 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
}

func runHTTPServer(server *http.Server) error {
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listenResult := make(chan error, 1)
	go func() {
		listenResult <- server.ListenAndServe()
	}()

	select {
	case err := <-listenResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-signalContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		err := <-listenResult
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func healthHandler(db *sql.DB, svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			log.Printf("health dependency=database status=unavailable error=%v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "data": gin.H{"status": "unavailable"}, "msg": "database unavailable"})
			return
		}
		if err := svc.ValidateStartupRuntime(); err != nil {
			log.Printf("health dependency=startup_runtime status=unavailable error=%v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "data": gin.H{"status": "unavailable"}, "msg": "startup runtime unavailable"})
			return
		}
		if err := svc.CheckRuntime(ctx); err != nil {
			log.Printf("health dependency=runtime status=unavailable error=%v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "data": gin.H{"status": "unavailable"}, "msg": "runtime coordinator unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"status":  "ok",
				"version": buildinfo.Version,
				"commit":  buildinfo.Commit,
			},
			"msg": "ok",
		})
	}
}

func requestLogger(writer io.Writer) gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		Output:          writer,
		SkipQueryString: true,
		Formatter: func(param gin.LogFormatterParams) string {
			return fmt.Sprintf(
				"%s - [%s] \"%s %s\" %d %s\n",
				param.ClientIP,
				param.TimeStamp.Format(time.RFC3339),
				param.Method,
				redactSensitiveRequestPath(param.Path),
				param.StatusCode,
				param.Latency,
			)
		},
	})
}

// requestRecovery 不使用 Gin 默认的 DumpRequest，因为异常时原始 URI 和 Referer 可能含 bearer 能力。
func requestRecovery(writer io.Writer) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				_, _ = fmt.Fprintf(
					writer,
					"panic recovered method=%s path=%s panic_type=%T\n%s",
					c.Request.Method,
					redactSensitiveRequestPath(c.Request.URL.Path),
					recovered,
					debug.Stack(),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

func redactSensitiveRequestPath(path string) string {
	path, _, _ = strings.Cut(path, "?")
	if strings.HasPrefix(path, "/pay/") {
		return "/pay/:token"
	}
	const checkoutPrefix = "/api/payments/checkout/"
	if strings.HasPrefix(path, checkoutPrefix) {
		remainder := strings.TrimPrefix(path, checkoutPrefix)
		if separator := strings.IndexByte(remainder, '/'); separator >= 0 && remainder[separator:] == "/transactions" {
			return checkoutPrefix + ":token/transactions"
		}
		return checkoutPrefix + ":token"
	}
	const canvasSharePrefix = "/api/public/canvas-shares/"
	if strings.HasPrefix(path, canvasSharePrefix) {
		remainder := strings.TrimPrefix(path, canvasSharePrefix)
		if separator := strings.IndexByte(remainder, '/'); separator >= 0 {
			return canvasSharePrefix + ":token" + remainder[separator:]
		}
		return canvasSharePrefix + ":token"
	}
	return path
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" && !allowedOrigin(c, origin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "data": nil, "msg": "不允许的跨域来源"})
			return
		}
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
		}
		c.Header("Access-Control-Allow-Headers", "Accept, Content-Type, Authorization, Idempotency-Key, X-Requested-With, X-Canvas-Scene, X-Idempotency-Key, X-Canvas-Upstream-URL, X-Canvas-Upstream-Format")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func allowedOrigin(_ *gin.Context, origin string) bool {
	environment := strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT"))
	if environment != "development" && environment != "production" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || strings.Contains(origin, "#") {
		return false
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if environment != "development" || !isLoopbackCORSHost(parsed.Hostname()) {
			return false
		}
	default:
		return false
	}
	for _, allowed := range strings.Split(os.Getenv("CANVAS_CORS_ORIGINS"), ",") {
		if strings.EqualFold(strings.TrimSpace(allowed), origin) {
			return true
		}
	}
	return false
}

func isLoopbackCORSHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
