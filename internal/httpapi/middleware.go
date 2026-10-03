package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func Logging(l *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		l.Printf("%d %-11s | %10s | %-4s %s",
			rec.status, http.StatusText(rec.status),
			fmt.Sprint(time.Since(start).Round(time.Microsecond)),
			r.Method, r.URL.Path)
	})
}
