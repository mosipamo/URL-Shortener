package httpapi

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func Logger(l *log.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		l.Printf("%d %-11s | %10s | %-4s %s",
			c.Writer.Status(),
			http.StatusText(c.Writer.Status()),
			fmt.Sprint(time.Since(start).Round(time.Microsecond)),
			c.Request.Method,
			c.Request.URL.Path,
		)
	}
}

func RateLimit(maxRequests int, window time.Duration) gin.HandlerFunc {
	if maxRequests <= 0 || window <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	var mu sync.Mutex
	clients := make(map[string][]time.Time)

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || c.FullPath() != "/api/shorten" {
			c.Next()
			return
		}

		ip := clientIP(c.Request)
		now := time.Now()
		cutoff := now.Add(-window)

		mu.Lock()
		bucket := clients[ip]
		filtered := bucket[:0]
		for _, ts := range bucket {
			if ts.After(cutoff) {
				filtered = append(filtered, ts)
			}
		}
		if len(filtered) >= maxRequests {
			clients[ip] = filtered
			mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}
		filtered = append(filtered, now)
		clients[ip] = filtered
		mu.Unlock()
		c.Next()
	}
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
