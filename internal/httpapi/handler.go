package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mosipamo/URL-Shortener/internal/store"
)

type Store interface {
	Save(longURL string) (code string, err error)
	Get(code string) (longURL store.Link, err error)
}

type server struct {
	store   Store
	baseURL string
}

func New(s Store, baseURL string) http.Handler {
	srv := &server{store: s, baseURL: strings.TrimRight(baseURL, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", srv.handleShorten)
	mux.HandleFunc("GET /api/v1/links/{code}", srv.handleLinkInfo)
	mux.HandleFunc("GET /{code}", srv.handleRedirect)
	return mux
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

func (s *server) handleShorten(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req shortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	code, err := s.store.Save(req.URL)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, shortenResponse{
		Code:     code,
		ShortURL: s.baseURL + "/" + code,
	})
}

func (s *server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	link, err := s.store.Get(r.PathValue("code"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	http.Redirect(w, r, link.URL, http.StatusFound)
}

func (s *server) handleLinkInfo(w http.ResponseWriter, r *http.Request) {
	link, err := s.store.Get(r.PathValue("code"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, linkResponse{
		URL:       link.URL,
		CreatedAt: link.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidURL):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	default:
		log.Printf("internal error: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
