package main

import (
	"context"
	"log"
	"net/http"
	"novaly/backend/config"
	"novaly/backend/database"
	"novaly/backend/localservice"
	"novaly/backend/routes"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := config.Load()
	db, err := database.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatal(err)
	}
	router := routes.New(db, cfg)
	manager := localservice.New()
	manager.Register(router)
	server := &http.Server{Addr: "127.0.0.1:" + cfg.Port, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Printf("Novaly Drama: http://127.0.0.1:%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Print(err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	if err := manager.Stop(); err != nil {
		log.Print(err)
	}
	_ = server.Shutdown(shutdown)
}
