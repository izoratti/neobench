package neobench

import (
	"os"
	"os/signal"
	"syscall"
)

/**
This func will setup signal handler channels.
- Listen to stopCh if you want to be notified of shutdown signals.
- Send one os.Signal on sigCh to start graceful shutdown.
- Send another to force exit.
*/
func SetupSignalHandler() (stopCh chan struct{}, stopFunc func()) {
	shutdownSignals := []os.Signal{os.Interrupt, syscall.SIGTERM}

	stopCh = make(chan struct{})
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, shutdownSignals...)

	stopFunc = func() {
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
		}
	}
	go func() {
		signalCount := 0

		select {
		case <-sigCh:
			signalCount++

			switch signalCount {
			case 1:
				stopFunc()
			case 2:
				os.Exit(1)
			}

		case <-stopCh:
			// Terminate goroutine
		}
	}()

	return stopCh, stopFunc
}
