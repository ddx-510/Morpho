package main

import (
	"flag"
	"log"
	"os"

	"github.com/ddx-510/Morpho/chat"
	"github.com/ddx-510/Morpho/config"
	"github.com/ddx-510/Morpho/ui"
)

func main() {
	configPath := flag.String("config", "morpho.json", "Path to config file")
	webMode := flag.Bool("web", false, "Serve chat UI on HTTP instead of terminal")
	port := flag.String("port", "8390", "Web UI port")
	workDir := flag.String("dir", ".", "Working directory for tools")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	provider, err := cfg.BuildProvider()
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	if *workDir == "." {
		*workDir, _ = os.Getwd()
	}

	app := chat.New(provider, cfg, *workDir)
	app.Cron.Start()
	defer app.Cron.Stop()

	if *webMode {
		ui.ServeWeb(app, *port)
	} else {
		ui.RunTerminal(app)
	}
}
