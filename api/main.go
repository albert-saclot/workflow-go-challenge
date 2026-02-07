package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"

	"workflow-code-test/api/pkg/clients/email"
	"workflow-code-test/api/pkg/clients/flood"
	"workflow-code-test/api/pkg/clients/sms"
	"workflow-code-test/api/pkg/clients/weather"
	"workflow-code-test/api/pkg/db"
	"workflow-code-test/api/services/nodes"
	"workflow-code-test/api/services/storage"
	"workflow-code-test/api/services/workflow"
)

func main() {
	ctx := context.Background()
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slog.SetDefault(slog.New(logHandler))

	dbURL, ok := os.LookupEnv("DATABASE_URL")
	if !ok {
		slog.Error("DATABASE_URL is not set")
		return
	}

	dbCfg := db.DefaultConfig(dbURL)
	pool, err := db.Connect(ctx, dbCfg)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return
	}
	defer pool.Close()

	// setup router
	mainRouter := mux.NewRouter()

	apiRouter := mainRouter.PathPrefix("/api/v1").Subrouter()

	pgStore, err := storage.NewInstance(pool)
	if err != nil {
		slog.Error("Failed to create store instance", "error", err)
		return
	}

	weatherClient := weather.NewOpenMeteoClient(nil)
	emailClient := email.NewStubClient("weather-alerts@example.com")
	smsClient := sms.NewStubClient()
	floodClient := flood.NewOpenMeteoClient(nil)
	deps := nodes.Deps{
		Weather: weatherClient,
		Email:   emailClient,
		SMS:     smsClient,
		Flood:   floodClient,
	}

	workflowService, err := workflow.NewService(pgStore, deps)
	if err != nil {
		slog.Error("Failed to create workflow service", "error", err)
		return
	}

	workflowService.LoadRoutes(apiRouter)

	corsHandler := handlers.CORS(
		// Frontend URL
		handlers.AllowedOrigins([]string{"http://localhost:3003"}),
		handlers.AllowedMethods([]string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
		handlers.AllowedHeaders([]string{"Content-Type", "Authorization"}),
		handlers.AllowCredentials(),
	)(mainRouter)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: corsHandler,
	}

	serverErrors := make(chan error, 1)

	go func() {
		slog.Info("Starting server on :8080")
		serverErrors <- srv.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		slog.Error("Server error", "error", err)

	case sig := <-shutdown:
		slog.Info("Shutdown signal received", "signal", sig)

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("Could not stop server gracefully", "error", err)
			srv.Close()
		}
	}
}
