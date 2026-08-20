// Package main is the entry point for the mail server.
// It initializes shared dependencies, starts concurrent listeners,
// and handles graceful shutdown. Zero protocol logic lives here.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"tinymail/internal/pop3"
	"tinymail/internal/smtp"
	"tinymail/internal/storage"
)

func main() {
	// --- Flag Parsing ---
	// Externalize configuration so ports/paths aren't hardcoded.
	// Enables running multiple instances or testing without recompilation.
	smtpPort := flag.String("smtp-port", "2525", "SMTP listener port")
	pop3Port := flag.String("pop3-port", "1100", "POP3 listener port")
	storageDir := flag.String("storage", "./storage", "Root storage directory")
	flag.Parse()

	// --- Initialize Shared Dependencies ---
	// Storage backend is created once and injected into both servers.
	// This ensures consistent atomic write behavior across protocols.

	store := storage.NewPOSIXStorage(*storageDir)
	log.Printf("[MAIN] Storage initialized at %s", *storageDir)

	// -- Start concurrect listeners
	// each protocol runs in its own goroutine with independent accept logs
	// sync.WaitGroup tracks active listners for clean shutdown coordination
	var wg sync.WaitGroup

	// SMTP Listner
	wg.Add(1)
	go func() {
		defer wg.Done()
		addr := ":" + *smtpPort
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal("[SMTP] Failed to listen on %s: %v", addr, err)
		}
		log.Printf("[SMPTP] listening on %s", addr)

		// accept loop: each connection spawns a goroutine
		// session.Run() owns conn lifecycle via defer; no shared state
		for {
			conn, err := listener.Accept()
			if err != nil {
				// listener closed during shutdown -> exit gracefully
				log.Printf("[SMTP] accept error (likely shutdown): %v", err)
				return
			}
			go smtp.NewSMTPSession(conn, store).Run()
		}
	}()

	// POP3 Listener
	wg.Add(1)
	go func() {
		defer wg.Done()
		addr := ":" + *pop3Port
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("[POP3] failed to listen on %s: %v", addr, err)
		}
		log.Printf("[POP3] listening on %s", addr)

		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("[POP3] accept error , likely shutdown: %v", err)
			}
			go pop3.NewPOP3Session(conn, store).Run()
		}
	}()

	// --- graceful shutdown signal handler
	// catches ctrl+c (sigint) and kill (sigterm)
	// stops accepting new connections , active sessions drain natually
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	fmt.Printf("\n[MAIN] recieved signal: %v. shuttind down..\n", sig)

	// TODO: add proper listener closing + session draining.
	// For now, this prevents abrupt process termination and logs intent.
	log.Println("[MAIN] Waiting for active sessions to complete...")
	wg.Wait() // Blocks until both listener goroutines return
	log.Println("[MAIN] Shutdown complete")

}
