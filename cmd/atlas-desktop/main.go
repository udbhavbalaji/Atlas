//go:build linux && cgo

package main

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <stdlib.h>
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

static void atlas_window_destroy(GtkWidget *widget, gpointer data) {
    gtk_main_quit();
}

static gboolean atlas_quit_on_idle(gpointer data) {
    gtk_main_quit();
    return G_SOURCE_REMOVE;
}

static void atlas_request_close() {
    g_idle_add(atlas_quit_on_idle, NULL);
}

static gboolean atlas_open_window(const char *url) {
    int argc = 0;
    char **argv = NULL;
    if (!gtk_init_check(&argc, &argv)) return FALSE;
    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), "Atlas");
    gtk_window_set_default_size(GTK_WINDOW(window), 1080, 820);
    WebKitWebView *view = WEBKIT_WEB_VIEW(webkit_web_view_new());
    gtk_container_add(GTK_CONTAINER(window), GTK_WIDGET(view));
    g_signal_connect(window, "destroy", G_CALLBACK(atlas_window_destroy), NULL);
    webkit_web_view_load_uri(view, url);
    gtk_widget_show_all(window);
    gtk_main();
    return TRUE;
}
*/
import "C"

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	runtime.LockOSThread()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Atlas desktop:", err)
		os.Exit(1)
	}
}

func run() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	serverBinary := filepath.Join(filepath.Dir(executable), "atlas-server")
	if _, err := os.Stat(serverBinary); err != nil {
		return fmt.Errorf("server binary is missing beside the desktop app: %w", err)
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(userHome(), ".local", "share")
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(userHome(), ".config")
	}
	dataDir := filepath.Join(dataHome, "atlas")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	command := exec.Command(serverBinary,
		"-addr", "127.0.0.1:0",
		"-db", filepath.Join(dataDir, "atlas.db"),
		"-openrouter-key-file", filepath.Join(configHome, "atlas", "openrouter.key"),
		"-announce-url")
	command.Stderr = os.Stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	defer stopServer(command)
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
			return
		}
		ready <- ""
	}()
	var announcement string
	select {
	case announcement = <-ready:
	case <-time.After(15 * time.Second):
		return errors.New("server startup timed out")
	}
	if !strings.HasPrefix(announcement, "ATLAS_URL=http://127.0.0.1:") {
		return fmt.Errorf("server did not announce a loopback URL: %q", announcement)
	}
	url := strings.TrimPrefix(announcement, "ATLAS_URL=")
	client := http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get(url + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			return errors.New("server did not become healthy")
		}
		time.Sleep(100 * time.Millisecond)
	}
	windowURL := C.CString(url + "/desktop")
	defer C.free(unsafe.Pointer(windowURL))
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		<-signals
		C.atlas_request_close()
	}()
	if C.atlas_open_window(windowURL) == 0 {
		return errors.New("GTK could not connect to a desktop display")
	}
	return nil
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func stopServer(command *exec.Cmd) {
	if command.Process == nil {
		return
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}
