// Package main is the entry point for the mail server.
// It initializes shared dependencies, starts concurrent listeners,
// and handles graceful shutdown. Zero protocol logic lives here.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

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

	// context for coordination graceful shutdown
	// canelling this context signals all istenrs to stop aceepting
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// -- Start concurrect listeners
	// each protocol runs in its own goroutine with independent accept logs
	// sync.WaitGroup tracks active listners for clean shutdown coordination
	var wg sync.WaitGroup

	// track listeners to be able to clsoe them on signal
	var listenersMu sync.Mutex
	listeners := make([]net.Listener, 0, 2)

	registerListener := func(l net.Listener) {
		listenersMu.Lock()
		listeners = append(listeners, l)
		listenersMu.Unlock()
	}

	// SMTP Listner
	wg.Add(1)
	go func() {
		defer wg.Done()
		addr := ":" + *smtpPort
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal("[SMTP] Failed to listen on %s: %v", addr, err)
		}
		registerListener(listener) // register for shutdown
		log.Printf("[SMPTP] listening on %s", addr)

		// accept loop: each connection spawns a goroutine
		// session.Run() owns conn lifecycle via defer; no shared state
		for {
			conn, err := listener.Accept()
			if err != nil {
				// listener closed during shutdown -> exit gracefully
				select {

				case <-ctx.Done():
					log.Printf("[SMTP] listener clsoed by shutdown signal")
					return
				default:
					log.Printf("[SMTP] Accept error: %v", err)
					continue
				}
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
		registerListener(listener) // register for sthudown
		log.Printf("[POP3] listening on %s", addr)

		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					log.Println("[POP3] Listener closed by shutdown signal")
					return
				default:
					log.Printf("[POP3] Accept error: %v", err)
					continue
				}
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

	cancel()
	listenersMu.Lock()
	for _, l := range listeners {
		l.Close() // forces accept() to return error immediately
	}
	listenersMu.Unlock()
	log.Println("[MAIN] stopped accepting new connections")

	// wait for active sessions to drain
	// active sessions have 30s timeout so they will self terminate with in that window without explicit cancecellation
	log.Println("[MAIN] waiting for active sessions to complete (max 30s)...")

	// timeout to prevent hanging if sessions ignores deadlines
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[MAIN] All sessions completed ")
	case <-time.After(35 * time.Second):
		log.Println("[MAIN] shutdown timeout reached; forcing exit")
	}

	log.Println("[MAIN] Shutdown complete")

}
