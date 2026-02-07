package workflow

import (
	"fmt"
	"net/http"
	"workflow-code-test/api/services/storage"

	"github.com/gorilla/mux"
)

// Service handles HTTP requests for workflow operations.
// It depends on the Storage interface rather than a concrete implementation,
// keeping the HTTP layer decoupled from persistence.
type Service struct {
	storage storage.Storage
}

// NewService creates a workflow Service with the given storage backend.
func NewService(store storage.Storage) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("service: store cannot be nil")
	}
	return &Service{storage: store}, nil
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
	router.Use(jsonMiddleware)

	router.HandleFunc("/{id}", s.HandleGetWorkflow).Methods("GET")
	router.HandleFunc("/{id}/execute", s.HandleExecuteWorkflow).Methods("POST")

}
