package main

import (
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/glimesh/broadcast-box/internal/chat"
	"github.com/glimesh/broadcast-box/internal/clips"
	"github.com/glimesh/broadcast-box/internal/console"
	"github.com/glimesh/broadcast-box/internal/emotes"
	"github.com/glimesh/broadcast-box/internal/environment"
	"github.com/glimesh/broadcast-box/internal/gifs"
	"github.com/glimesh/broadcast-box/internal/networktest"
	"github.com/glimesh/broadcast-box/internal/notify"
	"github.com/glimesh/broadcast-box/internal/server"
	"github.com/glimesh/broadcast-box/internal/webrtc"

	"net/http"
	_ "net/http/pprof"
)

func main() {
	environment.SetupLogger()
	environment.LoadEnvironmentVariables()
	console.HandleConsoleFlags()

	if shouldProfileApplication := os.Getenv(environment.EnableProfiling); strings.EqualFold(shouldProfileApplication, "true") {
		go func() {
			runtime.SetBlockProfileRate(1)
			runtime.SetMutexProfileFraction(1)
			slog.Error("pprof server exited", "err", http.ListenAndServe("localhost:6060", nil))
		}()
	}

	slog.Info("Booting up Broadcast Box")

	notify.Setup()
	clips.Setup()
	emotes.Setup()
	gifs.Setup()

	chatManager := chat.NewManager()
	webrtc.Setup(chatManager)

	if shouldNetworkTest := os.Getenv(environment.NetworkTestOnStart); strings.EqualFold(shouldNetworkTest, "true") {
		networktest.RunNetworkTest()
	}

	server.StartWebServer()
}
