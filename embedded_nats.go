package main

import (
	"fmt"
	"os"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

const embedNATSVar = "SMT_EMBED_NATS"

func startEmbeddedNATS() (func(), error) {
	if os.Getenv(embedNATSVar) != "1" {
		return func() {}, nil
	}
	server, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		return func() {}, fmt.Errorf("start embedded NATS: create server: %w", err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		server.Shutdown()
		return func() {}, fmt.Errorf("start embedded NATS: server did not become ready within 10 seconds")
	}
	if err := os.Setenv(natsURLVar, server.ClientURL()); err != nil {
		server.Shutdown()
		return func() {}, fmt.Errorf("start embedded NATS: set %s: %w", natsURLVar, err)
	}
	// Child object processes inherit the broker URL but must not start another broker.
	if err := os.Unsetenv(embedNATSVar); err != nil {
		server.Shutdown()
		return func() {}, fmt.Errorf("start embedded NATS: clear %s for child processes: %w", embedNATSVar, err)
	}
	return server.Shutdown, nil
}
