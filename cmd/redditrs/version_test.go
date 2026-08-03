package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		info     *debug.BuildInfo
		want     string
	}{
		{
			name:     "goreleaser ldflags take precedence",
			injected: "1.2.3",
			info:     &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}},
			want:     "1.2.3",
		},
		{
			name:     "go install module version",
			injected: "dev",
			info:     &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}},
			want:     "1.2.3",
		},
		{
			name:     "local development build",
			injected: "dev",
			info:     &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want:     "dev",
		},
		{
			name:     "missing build info",
			injected: "dev",
			want:     "dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.injected, tt.info); got != tt.want {
				t.Fatalf("resolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
