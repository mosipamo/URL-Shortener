package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mosipamo/URL-Shortener/internal/model"
	"github.com/mosipamo/URL-Shortener/internal/repository"
)

type server struct {
	repo    repository.Repository
	baseURL string
}

// New builds and returns a *gin.Engine (which satisfies http.Handler) with all
// routes registered and the Logger middleware attached. The caller supplies a
// *log.Logger so that tests can capture output without touching global state.
func New(repo repository.Repository, baseURL string, l *log.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Logger(l))

	s := &server{repo: repo, baseURL: strings.TrimRight(baseURL, "/")}
	r.POST("/api/shorten", s.handleShorten)
	r.GET("/api/v1/links/:code", s.handleLinkInfo)
	r.GET("/:code", s.handleRedirect)
	return r
}

// ── request / response shapes ────────────────────────────────────────────────

type shortenRequest struct {
	URL string `json:"url"`
}

type shortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

type linkResponse struct {
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

// ── handlers ─────────────────────────────────────────────────────────────────

func (s *server) handleShorten(c *gin.Context) {
	// Limit request body to 1 MiB to prevent resource exhaustion.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)

	var req shortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	code, err := s.repo.Save(req.URL)
	if err != nil {
		s.writeStoreError(c, err)
		return
	}

	c.JSON(http.StatusCreated, shortenResponse{
		Code:     code,
		ShortURL: s.baseURL + "/" + code,
	})
}

func (s *server) handleRedirect(c *gin.Context) {
	link, err := s.repo.Get(c.Param("code"))
	if err != nil {
		s.writeStoreError(c, err)
		return
	}
	c.Redirect(http.StatusFound, link.URL)
}

func (s *server) handleLinkInfo(c *gin.Context) {
	link, err := s.repo.Get(c.Param("code"))
	if err != nil {
		s.writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, linkResponse{
		URL:       link.URL,
		CreatedAt: link.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// ── error mapping ─────────────────────────────────────────────────────────────

// writeStoreError maps domain sentinel errors to HTTP status codes and writes
// a JSON {"error":"…"} body. Internal errors are logged but not echoed to the
// client so that implementation details stay server-side.
func (s *server) writeStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidURL):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	default:
		log.Printf("internal error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}
