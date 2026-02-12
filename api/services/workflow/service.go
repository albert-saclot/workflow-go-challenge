package workflow

import (
	"context"
	"fmt"
	"net/http"
	"workflow-code-test/api/services/nodes"
	"workflow-code-test/api/services/storage"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type contextKey string

const requestIDKey contextKey = "requestID"

// Service handles HTTP requests for workflow operations.
// It depends on the Storage interface rather than a concrete implementation,
// keeping the HTTP layer decoupled from persistence.
type Service struct {
	storage storage.Storage
	deps    nodes.Deps
}

// NewService creates a workflow Service with the given storage backend
// and external client dependencies used during workflow execution.
func NewService(store storage.Storage, deps nodes.Deps) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("service: store cannot be nil")
	}
	return &Service{storage: store, deps: deps}, nil
}

// requestIDMiddleware assigns a unique ID to each request for log correlation.
// If the client sends X-Request-ID, it's reused; otherwise a new UUID is generated.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// jsonMiddleware sets the Content-Type header to application/json
func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func (s *Service) LoadRoutes(parentRouter *mux.Router) {
	router := parentRouter.PathPrefix("/workflows").Subrouter()
	router.StrictSlash(false)
	router.Use(requestIDMiddleware)
	router.Use(jsonMiddleware)

	router.HandleFunc("/{id}", s.HandleGetWorkflow).Methods("GET")
	router.HandleFunc("/{id}/execute", s.HandleExecuteWorkflow).Methods("POST")
	router.HandleFunc("/{id}/publish", s.HandlePublishWorkflow).Methods("POST")
}
