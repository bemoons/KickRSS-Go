package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"kickrss/config"
	"kickrss/db"
	"kickrss/routers"
	"kickrss/scheduler"
)

func main() {
	log.Println("Starting KickRSS Go Backend...")

	// 1. Load config
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	log.Printf("Loading configuration from: %s", configPath)
	if err := config.LoadConfig(configPath); err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 2. Initialize database
	if err := db.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.CloseDB()

	// 3. Start scheduler
	scheduler.StartScheduler()
	defer scheduler.ShutdownScheduler()

	// 4. Setup Gin HTTP router
	r := routers.SetupRouter()

	// Determine port
	portStr := os.Getenv("PORT")
	port := 8888
	if portStr != "" {
		if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
			log.Printf("Invalid PORT environment variable %q, using config or default port", portStr)
			port = config.GlobalConfig.Port
		}
	} else {
		port = config.GlobalConfig.Port
	}
	if port == 0 {
		port = 8888
	}

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("Listening and serving HTTP on %s", addr)

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Initializing the server in a goroutine so that
	// it won't block the graceful shutdown handling below
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// 5. Graceful shutdown setup
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be caught, so no need to add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown: ", err)
	}

	log.Println("Server exiting")
}
