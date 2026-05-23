package systrayapp

import (
	_ "embed"
	"html/template"
	"net/http"
	"strings"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/logbuf"

	"github.com/gin-gonic/gin"
)

//go:embed logview.html
var logviewHTML string

var logviewTmpl = template.Must(template.New("logview").Parse(logviewHTML))

// LogViewData is the template data for the log viewer page.
type LogViewData struct {
	Logs string
}

// RegisterLogViewRoute adds GET /_logs to the gin engine (protected by API_PASSWORD when set).
func RegisterLogViewRoute(r *gin.Engine, mw *logbuf.MultiWriter) {
	logsGroup := r.Group("/")
	logsGroup.Use(auth.APIPasswordMiddleware())
	logsGroup.GET("/_logs", func(c *gin.Context) {
		lines := mw.Lines()
		data := LogViewData{
			Logs: strings.Join(lines, "\n"),
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		if err := logviewTmpl.Execute(c.Writer, data); err != nil {
			c.String(http.StatusInternalServerError, "template error")
		}
	})
}
