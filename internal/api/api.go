package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/bugsnag/bugsnag-go/v2"
	"github.com/go-redis/redis/v8"
	"github.com/gofrs/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sideshow/apns2/token"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	"github.com/christianselig/apollo-backend/internal/domain"
	"github.com/christianselig/apollo-backend/internal/reddit"
	"github.com/christianselig/apollo-backend/internal/repository"
)

type api struct {
	logger     *zap.Logger
	statsd     *statsd.Client
	reddit     *reddit.Client
	apns       *token.Token
	httpClient *http.Client

	accountRepo      domain.AccountRepository
	deviceRepo       domain.DeviceRepository
	subredditRepo    domain.SubredditRepository
	watcherRepo      domain.WatcherRepository
	userRepo         domain.UserRepository
	liveActivityRepo domain.LiveActivityRepository
}

func NewAPI(ctx context.Context, logger *zap.Logger, statsd *statsd.Client, redis *redis.Client, pool *pgxpool.Pool) *api {
	tracer := otel.Tracer("api")

	reddit := reddit.NewClient(
		os.Getenv("REDDIT_CLIENT_ID"),
		os.Getenv("REDDIT_CLIENT_SECRET"),
		tracer,
		statsd,
		redis,
		16,
	)

	var apns *token.Token
	{
		authKey, err := token.AuthKeyFromFile(os.Getenv("APPLE_KEY_PATH"))
		if err != nil {
			panic(err)
		}

		apns = &token.Token{
			AuthKey: authKey,
			KeyID:   os.Getenv("APPLE_KEY_ID"),
			TeamID:  os.Getenv("APPLE_TEAM_ID"),
		}
	}

	accountRepo := repository.NewPostgresAccount(pool)
	deviceRepo := repository.NewPostgresDevice(pool)
	subredditRepo := repository.NewPostgresSubreddit(pool)
	watcherRepo := repository.NewPostgresWatcher(pool)
	userRepo := repository.NewPostgresUser(pool)
	liveActivityRepo := repository.NewPostgresLiveActivity(pool)

	client := &http.Client{}

	return &api{
		logger:     logger,
		statsd:     statsd,
		reddit:     reddit,
		apns:       apns,
		httpClient: client,

		accountRepo:      accountRepo,
		deviceRepo:       deviceRepo,
		subredditRepo:    subredditRepo,
		watcherRepo:      watcherRepo,
		userRepo:         userRepo,
		liveActivityRepo: liveActivityRepo,
	}
}

func (a *api) Server(port int) *http.Server {
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: bugsnag.Handler(a.Routes()),
	}
}

func (a *api) Routes() *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/v1/health", a.healthCheckHandler).Methods("GET")

	r.HandleFunc("/v1/device", a.upsertDeviceHandler).Methods("POST")
	r.HandleFunc("/v1/device/{apns}", a.deleteDeviceHandler).Methods("DELETE")
	r.HandleFunc("/v1/device/{apns}/test", a.testDeviceHandler).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/test/comment_reply", generateNotificationTester(a, commentReply)).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/test/post_reply", generateNotificationTester(a, postReply)).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/test/private_message", generateNotificationTester(a, privateMessage)).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/test/subreddit_watcher", generateNotificationTester(a, subredditWatcher)).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/test/trending_post", generateNotificationTester(a, trendingPost)).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/test/username_mention", generateNotificationTester(a, usernameMention)).Methods("POST")

	r.HandleFunc("/v1/device/{apns}/account", a.upsertAccountHandler).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/accounts", a.upsertAccountsHandler).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/account/{redditID}", a.disassociateAccountHandler).Methods("DELETE")
	r.HandleFunc("/v1/device/{apns}/account/{redditID}/notifications", a.notificationsAccountHandler).Methods("PATCH")
	r.HandleFunc("/v1/device/{apns}/account/{redditID}/notifications", a.getNotificationsAccountHandler).Methods("GET")

	r.HandleFunc("/v1/device/{apns}/account/{redditID}/watcher", a.createWatcherHandler).Methods("POST")
	r.HandleFunc("/v1/device/{apns}/account/{redditID}/watcher/{watcherID}", a.deleteWatcherHandler).Methods("DELETE")
	r.HandleFunc("/v1/device/{apns}/account/{redditID}/watcher/{watcherID}", a.editWatcherHandler).Methods("PATCH")
	r.HandleFunc("/v1/device/{apns}/account/{redditID}/watchers", a.listWatchersHandler).Methods("GET")

	r.HandleFunc("/v1/live_activities", a.createLiveActivityHandler).Methods("POST")

	r.HandleFunc("/v1/receipt", a.checkReceiptHandler).Methods("POST")
	r.HandleFunc("/v1/receipt/{apns}", a.checkReceiptHandler).Methods("POST")

	r.HandleFunc("/v1/contact", a.contactHandler).Methods("POST")

	r.HandleFunc("/v1/test/bugsnag", a.testBugsnagHandler).Methods("POST")

	r.Use(a.loggingMiddleware)
	r.Use(a.requestIdMiddleware)

	return r
}

func (a *api) testBugsnagHandler(w http.ResponseWriter, r *http.Request) {
	if err := bugsnag.Notify(fmt.Errorf("Test error")); err != nil {
		a.errorResponse(w, r, 500, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type LoggingResponseWriter struct {
	w          http.ResponseWriter
	statusCode int
	bytes      int
}

func (lrw *LoggingResponseWriter) Header() http.Header {
	return lrw.w.Header()
}

func (lrw *LoggingResponseWriter) Write(bb []byte) (int, error) {
	wb, err := lrw.w.Write(bb)
	lrw.bytes += wb
	return wb, err
}

func (lrw *LoggingResponseWriter) WriteHeader(statusCode int) {
	lrw.w.WriteHeader(statusCode)
	lrw.statusCode = statusCode
}

func (a *api) requestIdMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.Must(uuid.NewV4()).String()
		w.Header().Set("X-Apollo-Request-Id", id)
		next.ServeHTTP(w, r)
	})
}

func (a *api) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip logging health checks
		if r.RequestURI == "/v1/health" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		lrw := &LoggingResponseWriter{w: w}

		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(lrw, r)

		duration := time.Since(start).Milliseconds()

		remoteAddr := r.Header.Get("X-Forwarded-For")
		if remoteAddr == "" {
			if ip, _, err := net.SplitHostPort(r.RemoteAddr); err != nil {
				remoteAddr = "unknown"
			} else {
				remoteAddr = ip
			}
		}

		fields := []zap.Field{
			zap.Int64("duration", duration),
			zap.String("method", r.Method),
			zap.String("remote#addr", remoteAddr),
			zap.Int("response#bytes", lrw.bytes),
			zap.Int("status", lrw.statusCode),
			zap.String("uri", r.RequestURI),
			zap.String("request#id", lrw.Header().Get("X-Apollo-Request-Id")),
		}

		if lrw.statusCode == 200 {
			a.logger.Info("", fields...)
		} else {
			err := lrw.Header().Get("X-Apollo-Error")
			a.logger.Error(err, fields...)
		}

		tags := []string{fmt.Sprintf("status:%d", lrw.statusCode)}
		_ = a.statsd.Histogram("api.latency", float64(duration), nil, 1.0)
		_ = a.statsd.Incr("api.calls", tags, 1.0)
		if lrw.statusCode >= 500 {
			_ = a.statsd.Incr("api.errors", nil, 1.0)
		}
	})
}
