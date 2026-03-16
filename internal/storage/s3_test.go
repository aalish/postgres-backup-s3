package storage

import "testing"

func TestBuildObjectKey(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		filename string
		want     string
	}{
		{name: "no prefix", prefix: "", filename: "file.dump", want: "file.dump"},
		{name: "trim slash", prefix: "/nightly/main/", filename: "file.dump", want: "nightly/main/file.dump"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildObjectKey(tt.prefix, tt.filename); got != tt.want {
				t.Fatalf("BuildObjectKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
