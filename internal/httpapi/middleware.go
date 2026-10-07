package httpapi

import (
	"fmt"
	"log"
	"net/http"
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
