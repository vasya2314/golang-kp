package core_http_server

import (
	"fmt"

	"github.com/go-chi/chi/v5"
)

type ApiVersion string

var (
	ApiVersion1 = ApiVersion("v1")
)

type APIVersionRouter struct {
	*chi.Mux
	version ApiVersion
}

func NewAPIVersionRouter(version ApiVersion) *APIVersionRouter {
	return &APIVersionRouter{
		Mux:     chi.NewRouter(),
		version: version,
	}
}

func (r *APIVersionRouter) RegisterRoutes(router *chi.Mux) {
	router.Mount(fmt.Sprintf("/api/%s", r.version), r.Mux)
}
