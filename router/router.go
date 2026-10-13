// Package router assembles every HTTP surface of the gateway.
package router

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/controller"
	"github.com/lxp/htokenmeter/middleware"
	"github.com/lxp/htokenmeter/relay/dto"
	"github.com/lxp/htokenmeter/relay/helper"
)

// SetRouter wires everything. webFS may be nil in dev (no embedded console).
func SetRouter(r *gin.Engine, webFS fs.FS) {
	setRelayRouter(r)
	setAPIRouter(r)
	setWebRouter(r, webFS)
}

func setRelayRouter(r *gin.Engine) {
	// Models listing in three SDK shapes.
	models := r.Group("/v1/models")
	models.Use(middleware.TokenAuth())
	{
		models.GET("", controller.ListModels)
		models.GET("/:model", controller.RetrieveModel)
	}
	geminiModels := r.Group("/v1beta/models")
	geminiModels.Use(middleware.TokenAuth())
	{
		geminiModels.GET("", controller.ListGeminiModels)
	}

	v1 := r.Group("/v1")
	v1.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		v1.POST("/chat/completions", func(c *gin.Context) { controller.Relay(c, helper.FormatOpenAI) })
		v1.POST("/completions", func(c *gin.Context) { controller.Relay(c, helper.FormatOpenAI) })
		v1.POST("/embeddings", func(c *gin.Context) { controller.Relay(c, helper.FormatOpenAI) })
		v1.POST("/images/generations", func(c *gin.Context) { controller.Relay(c, helper.FormatOpenAI) })
		v1.POST("/moderations", func(c *gin.Context) { controller.Relay(c, helper.FormatOpenAI) })
		v1.POST("/messages", func(c *gin.Context) { controller.Relay(c, helper.FormatClaude) })
		v1.POST("/responses", func(c *gin.Context) { controller.Relay(c, helper.FormatResponses) })
	}

	// GET /v1/responses/:id carries no request body, so Distribute's
	// model requirement cannot apply; TokenAuth alone guards retrieval
	// and pickChannel falls back to any openai-compatible channel.
	v1Get := r.Group("/v1")
	v1Get.Use(middleware.TokenAuth())
	{
		v1Get.GET("/responses/:id", func(c *gin.Context) { controller.Relay(c, helper.FormatResponses) })
	}

	v1beta := r.Group("/v1beta")
	v1beta.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		v1beta.POST("/models/*path", func(c *gin.Context) { controller.Relay(c, helper.FormatGemini) })
	}
}

func setAPIRouter(r *gin.Engine) {
	api := r.Group("/api")
	{
		api.GET("/status", controller.GetStatus)

		user := api.Group("/user")
		{
			user.POST("/login", controller.Login)
			user.POST("/logout", controller.Logout)
			self := user.Group("/")
			self.Use(middleware.UserAuth())
			{
				self.POST("/password", controller.ChangePassword)
			}
		}

		admin := api.Group("/")
		admin.Use(middleware.AdminAuth())
		{
			admin.GET("/channel", controller.ListChannels)
			admin.POST("/channel", controller.CreateChannel)
			admin.PUT("/channel", controller.UpdateChannel)
			admin.DELETE("/channel/:id", controller.DeleteChannel)
			admin.POST("/channel/:id/status", controller.UpdateChannelStatus)
			admin.GET("/channel/:id/test", controller.TestChannel)

			admin.GET("/user", controller.ListUsers)
			admin.POST("/user", controller.CreateUser)
			admin.PUT("/user", controller.UpdateUser)
			admin.DELETE("/user/:id", controller.DeleteUser)

			admin.GET("/price", controller.ListPrices)
			admin.POST("/price", controller.SavePrice)
			admin.DELETE("/price/:model", controller.DeletePrice)

			admin.GET("/option", controller.GetOptions)
			admin.POST("/option", controller.SetOption)
		}

		authed := api.Group("/")
		authed.Use(middleware.UserAuth())
		{
			authed.GET("/token", controller.ListTokens)
			authed.POST("/token", controller.CreateToken)
			authed.PUT("/token", controller.UpdateToken)
			authed.DELETE("/token/:id", controller.DeleteToken)
			authed.GET("/log", controller.ListLogs)
			authed.GET("/stats", controller.GetStats)
		}
	}
}

func setWebRouter(r *gin.Engine, webFS fs.FS) {
	if webFS == nil {
		r.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
				"message": "console not embedded; build web/ first", "type": "gateway_error",
			}})
		})
		return
	}
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
				"message": "invalid URL (" + c.Request.Method + " " + path + ")", "type": "invalid_request_error",
			}})
			return
		}
		// SPA: serve static assets; everything else falls back to index.html.
		f, err := webFS.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			defer f.Close()
			c.FileFromFS(strings.TrimPrefix(path, "/"), http.FS(webFS))
			return
		}
		index, err := webFS.Open("index.html")
		if err != nil {
			c.String(http.StatusNotFound, "console index missing")
			return
		}
		defer index.Close()
		c.Header("Cache-Control", "no-cache")
		data, _ := readAllFS(index)
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})
}

func readAllFS(f fs.File) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			out = append(out, buf[:n]...)
		}
		if err != nil {
			return out, nil
		}
	}
}

// EmbedWeb returns the embedded console filesystem, or nil when the build
// tag excluded it (dev builds).
func EmbedWeb(buildFS embed.FS) fs.FS {
	sub, err := fs.Sub(buildFS, "web/dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

var _ = dto.ModelCard{}
