package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/mosipamo/URL-Shortener.git/internal/store"
)

type Store interface {
	Save(longURL string) (code string, err error)
	Get(code string) (longURL string, err error)
}

type server struct {
	store   Store
	baseURL string
}

func New(s Store, baseURL string) http.Handler {
	srv := &server{store: s, baseURL: strings.TrimRight(baseURL, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", srv.handleShorten)
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

func (s *server) handleShorten(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req shortenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	code, err := s.store.Save(req.URL)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidURL):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			log.Printf("shorten: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusCreated, shortenResponse{
		Code:     code,
		ShortURL: s.baseURL + "/" + code,
	})
}

func (s *server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	longURL, err := s.store.Get(r.PathValue("code"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		log.Printf("redirect: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.Redirect(w, r, longURL, http.StatusFound)
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
