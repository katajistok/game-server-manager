package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	token := getEnv("AGENT_TOKEN", "")
	if savedToken, err := os.ReadFile("/app/agent.token"); err == nil && len(savedToken) > 0 {
		token = string(savedToken)
		log.Printf("[agent] Loaded token from agent.token file")
	}

	cfg := Config{
		MasterURL: getEnv("MASTER_URL", "ws://localhost:8080/ws/agent"),
		AgentName: getEnv("AGENT_NAME", "default-agent"),
		Token:     token,
	}

	log.Printf("[agent] Starting agent: %s", cfg.AgentName)
	log.Printf("[agent] Connecting to master: %s", cfg.MasterURL)

	var agent *Agent

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Printf("[agent] Shutting down...")
		if agent != nil {
			agent.Stop()
		}
		os.Exit(0)
	}()

	for {
		log.Printf("[agent] Attempting connection...")
		agent = NewAgent(cfg)
		err := agent.Connect()
		if err != nil {
			log.Printf("[agent] Connection failed: %v", err)
		}
		log.Printf("[agent] Reconnecting in 5 seconds...")
		time.Sleep(5 * time.Second)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
