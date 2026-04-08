package http

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:web/dist
var frontendAssets embed.FS

func registerFrontendRoutes(router *gin.Engine) {
	distFS, err := fs.Sub(frontendAssets, "web/dist")
	if err != nil {
		return
	}

	fileServer := http.FileServer(http.FS(distFS))

	router.GET("/", func(c *gin.Context) {
		serveFrontendAsset(c, distFS, "index.html")
	})

	router.NoRoute(func(c *gin.Context) {
		requestPath := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
		if strings.HasPrefix(requestPath, "api/") {
			c.JSON(http.StatusNotFound, gin.H{"message": "接口不存在"})
			return
		}

		if requestPath == "" || requestPath == "." {
			requestPath = "index.html"
		}

		if _, err := fs.Stat(distFS, requestPath); err == nil {
			c.Request.URL.Path = "/" + requestPath
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		serveFrontendAsset(c, distFS, "index.html")
	})
}

func serveFrontendAsset(c *gin.Context, assets fs.FS, filename string) {
	content, err := fs.ReadFile(assets, filename)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "页面资源不存在"})
		return
	}

	switch {
	case strings.HasSuffix(filename, ".html"):
		c.Data(http.StatusOK, "text/html; charset=utf-8", content)
	case strings.HasSuffix(filename, ".svg"):
		c.Data(http.StatusOK, "image/svg+xml", content)
	default:
		c.Data(http.StatusOK, "application/octet-stream", content)
	}
}
