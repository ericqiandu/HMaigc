package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"infinite-canvas/backend/internal/buildinfo"
	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/handler"
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
	if err := svc.ValidateRuntime(); err != nil {
		log.Fatal(err)
	}
	if err := svc.EnsureSystemChannelModels(); err != nil {
		log.Fatal(err)
	}
	if err := svc.EnsureDefaultStoryboardPromptTemplate(); err != nil {
		log.Fatal(err)
	}
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		log.Fatal(err)
	}
	if err := svc.EnsureBuiltinProjectWorkflowTemplate(); err != nil {
		log.Fatal(err)
	}
	if summary, err := svc.MigrateLegacyStorage(); err != nil {
		log.Printf("storage migration skipped after error: %v", err)
	} else if summary.Backup != "" {
		log.Printf("storage migration completed: tasks=%d assets=%d projects=%d backup=%s", summary.Tasks, summary.Assets, summary.Projects, summary.Backup)
	}
	svc.StartWorker()

	r := gin.New()
	r.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s - [%s] \"%s %s\" %d %s %s\n", param.ClientIP, param.TimeStamp.Format(time.RFC3339), param.Method, redactCanvasSharePath(param.Path), param.StatusCode, param.Latency, param.ErrorMessage)
	}), gin.Recovery())
	r.Use(cors())
	handler.ConfigureRuntime(svc)
	api := r.Group("/api")
	api.GET("/health", healthHandler(sqlDB, svc))
	handler.RegisterOAuthCallbackRoutes(r, svc)
	handler.RegisterAuthRoutes(api, svc)
	handler.RegisterAdminRoutes(api, svc)
	handler.RegisterAdminAnalyticsRoutes(api, svc)
	handler.RegisterAnnouncementRoutes(api, svc)
	handler.RegisterFinanceRoutes(api, svc)
	handler.RegisterMembershipRoutes(api, svc)
	handler.RegisterReferralRoutes(api, svc)
	handler.RegisterTeamRoutes(api, svc)
	handler.RegisterPaymentRoutes(api, svc)
	handler.RegisterSiteSettingRoutes(api, svc)
	// 登录态模型目录代理：避免浏览器直连各上游时分别处理 CORS。
	handler.RegisterChannelModelRoutes(api, svc)
	handler.RegisterSystemProxyRoutes(api, svc)
	handler.RegisterCustomRelayRoutes(api, svc)
	handler.RegisterTaskRoutes(api, svc)
	handler.RegisterSessionRoutes(api, svc)
	handler.RegisterSkillRoutes(api, svc)
	handler.RegisterUserDataRoutes(api, svc)
	handler.RegisterProjectRoutes(api, svc)
	handler.RegisterCanvasShareRoutes(api, svc)
	if err := handler.RegisterCanvasCollaborationRoutes(api, svc); err != nil {
		log.Fatal(err)
	}

	addr := env("CANVAS_BACKEND_ADDR", ":8080")
	log.Printf("HMaigc backend listening on %s", addr)
	if err := runHTTPServer(newHTTPServer(addr, r)); err != nil {
		log.Fatal(err)
	}
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

func redactCanvasSharePath(path string) string {
	const prefix = "/api/public/canvas-shares/"
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	remainder := strings.TrimPrefix(path, prefix)
	if index := strings.IndexByte(remainder, '/'); index >= 0 {
		return prefix + ":token" + remainder[index:]
	}
	return prefix + ":token"
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
		c.Header("Access-Control-Allow-Headers", "Accept, Content-Type, Authorization, X-Requested-With, X-Canvas-Scene, X-Idempotency-Key, X-Canvas-Upstream-URL, X-Canvas-Upstream-Format")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func allowedOrigin(c *gin.Context, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	requestHost := c.Request.Host
	if forwardedHost := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); forwardedHost != "" {
		requestHost = strings.TrimSpace(strings.Split(forwardedHost, ",")[0])
	}
	if strings.EqualFold(parsed.Host, strings.TrimSpace(requestHost)) {
		return true
	}
	for _, allowed := range strings.Split(os.Getenv("CANVAS_CORS_ORIGINS"), ",") {
		if strings.TrimSpace(allowed) == "*" {
			return true
		}
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(allowed), "/"), strings.TrimRight(origin, "/")) {
			return true
		}
	}
	host := strings.ToLower(parsed.Hostname())
	return (host == "localhost" || host == "127.0.0.1" || host == "::1") && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
