package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"boot.dev/linko/internal/store"
)

const logContextKey contextKey = "log_context"

type LogContext struct {
	Username string
	Error    error
}

func httpError(ctx context.Context, w http.ResponseWriter, status int, err error) {
	if logCtx, ok := ctx.Value(logContextKey).(*LogContext); ok {
		logCtx.Error = err
	}
	http.Error(w, err.Error(), status)
}

func requestLogger(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			logContext := &LogContext{}
			r = r.WithContext(context.WithValue(r.Context(), logContextKey, logContext))

			requestid := w.Header().Get("X-Request-Id")

			spyW := &spyResponseWriter{ResponseWriter: w}
			spyR := &spyReadCloser{ReadCloser: r.Body}
			r.Body = spyR
			next.ServeHTTP(spyW, r)
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("client_ip", r.RemoteAddr),
				slog.Duration("duration", time.Since(start)),
				slog.Int("request_body_bytes", spyR.requestBodyBytes),
				slog.Int("response_status", spyW.responseStatus),
				slog.Int("response_body_bytes", spyW.responseBodyBytes),
				slog.String("request_id", requestid),
			}

			if logContext.Username != "" {
				attrs = append(attrs, slog.String("user", logContext.Username))
			}

			if logContext.Error != nil {
				attrs = append(attrs, slog.GroupAttrs("error", errorAttrs(logContext.Error)...))
			}

			logger.LogAttrs(r.Context(), slog.LevelInfo, "Served request", attrs...)
		})
	}
}

type server struct {
	httpServer *http.Server
	store      store.Store
	cancel     context.CancelFunc
	logger     *slog.Logger
}

func chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

func newServer(store store.Store, port int, cancel context.CancelFunc, logger *slog.Logger) *server {
	mux := http.NewServeMux()

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: chain(mux, requestID(), requestLogger(logger)),
	}

	s := &server{
		httpServer: srv,
		store:      store,
		cancel:     cancel,
		logger:     logger,
	}

	mux.HandleFunc("GET /", s.handlerIndex)
	mux.Handle("POST /api/login", s.authMiddleware(http.HandlerFunc(s.handlerLogin)))
	mux.Handle("POST /api/shorten", s.authMiddleware(http.HandlerFunc(s.handlerShortenLink)))
	mux.Handle("GET /api/stats", s.authMiddleware(http.HandlerFunc(s.handlerStats)))
	mux.Handle("GET /api/urls", s.authMiddleware(http.HandlerFunc(s.handlerListURLs)))
	mux.HandleFunc("GET /{shortCode}", s.handlerRedirect)
	mux.HandleFunc("POST /admin/shutdown", s.handlerShutdown)

	return s
}

func requestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestId := r.Header.Get("X-Request-Id")
			if requestId == "" {
				requestId = rand.Text()
			}
			w.Header().Set("X-Request-ID", requestId)

			next.ServeHTTP(w, r)
		})
	}
}

func (s *server) start() error {
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return err
	}
	s.logger.Debug(fmt.Sprintf("Linko is running on http://localhost:%d", ln.Addr().(*net.TCPAddr).Port))
	if err := s.httpServer.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) shutdown(ctx context.Context) error {

	return s.httpServer.Shutdown(ctx)
}

func (s *server) handlerShutdown(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("ENV") == "production" {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	go s.cancel()
}
