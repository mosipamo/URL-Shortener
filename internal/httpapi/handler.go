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

func New(repo repository.Repository, baseURL string, l *log.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(Logger(l))

	s := &server{repo: repo, baseURL: strings.TrimRight(baseURL, "/")}
	router.POST("/api/shorten", s.handleShorten)
	router.GET("/api/v1/links/:code", s.handleLinkInfo)
	router.GET("/:code", s.handleRedirect)
	return router
}

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

func (s *server) handleShorten(c *gin.Context) {
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
