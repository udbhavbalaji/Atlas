//go:build linux && cgo

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredSharedURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-url")
	for _, tc := range []struct {
		name, env, file, want string
		invalid               bool
	}{
		{name: "local default"},
		{name: "private HTTPS file", file: "https://atlas.example.test/\n", want: "https://atlas.example.test"},
		{name: "environment wins", env: "https://shared.example.test", file: "https://atlas.example.test", want: "https://shared.example.test"},
		{name: "loopback testing", env: "http://127.0.0.1:8080/", want: "http://127.0.0.1:8080"},
		{name: "remote HTTP rejected", env: "http://atlas.example.test", invalid: true},
		{name: "credentials rejected", env: "https://user:secret@atlas.example.test", invalid: true},
		{name: "path rejected", env: "https://atlas.example.test/other", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ATLAS_SERVER_URL", tc.env)
			if tc.file == "" {
				_ = os.Remove(path)
			} else if err := os.WriteFile(path, []byte(tc.file), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := configuredSharedURL(path)
			if tc.invalid && err == nil || !tc.invalid && (err != nil || got != tc.want) {
				t.Fatalf("URL=%q error=%v, want %q invalid=%v", got, err, tc.want, tc.invalid)
			}
		})
	}
}
