package main

import (
	"embed"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lxp/htokenmeter/common"
	"github.com/lxp/htokenmeter/controller"
	"github.com/lxp/htokenmeter/model"
	"github.com/lxp/htokenmeter/router"
)

//go:embed all:web/dist
var buildFS embed.FS

func main() {
	version := flag.Bool("version", false, "print version and exit")
	port := flag.Int("port", common.EnvInt("PORT", 8317), "listen port")
	configDir := flag.String("config-dir", common.EnvStr("CONFIG_DIR", "/etc/htokenmeter"), "config directory")
	dbPath := flag.String("db", common.EnvStr("DB_PATH", ""), "sqlite path (default <config-dir>/htokenmeter.db)")
	logDir := flag.String("log-dir", common.EnvStr("LOG_DIR", ""), "log directory (default stderr)")
	flag.Parse()

	if *version {
		fmt.Println("htokenmeter", common.Version)
		return
	}

	common.ConfigDir = *configDir
	common.DBPath = *dbPath
	common.LogDir = *logDir
	common.Port = *port
	common.RetryTimes = common.EnvInt("RETRY_TIMES", 3)
	common.LogRetention = common.EnvInt("LOG_RETENTION_DAYS", 30)
	common.TestFrequency = common.EnvInt("TEST_FREQUENCY_MINUTES", 0)

	common.SetupLogger()
	common.Info("htokenmeter %s starting (config dir %s)", common.Version, common.ConfigDir)

	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}

	if err := model.InitDB(); err != nil {
		common.Fatal("database init failed: %v", err)
	}
	defer func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	if err := model.LoadOptions(); err != nil {
		common.Error("load options: %v", err)
	}

	controller.StartChannelTester()
	controller.StartLogCleaner()

	server := gin.New()
	server.Use(gin.Recovery())
	router.SetRouter(server, router.EmbedWeb(buildFS))

	addr := fmt.Sprintf(":%d", common.Port)
	httpSrv := &http.Server{
		Addr: addr, Handler: server,
		ReadHeaderTimeout: 30 * time.Second,
	}
	common.Info("listening on %s", addr)
	if err := httpSrv.ListenAndServe(); err != nil {
		common.Fatal("http server: %v", err)
	}
}
