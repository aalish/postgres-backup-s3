package backup

import (
	"testing"
	"time"
)

func TestBuildFilename(t *testing.T) {
	got := BuildFilename("prod/main db", time.Date(2026, 3, 16, 12, 30, 45, 0, time.FixedZone("UTC+5", 5*60*60)))
	want := "prod-main-db_20260316T073045Z.dump"

	if got != want {
		t.Fatalf("BuildFilename() = %q, want %q", got, want)
	}
}

func TestBuildFilenameFallsBackToDefaultPrefix(t *testing.T) {
	got := BuildFilename(" / ", time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC))
	want := "postgres-backup_20260316T000000Z.dump"

	if got != want {
		t.Fatalf("BuildFilename() = %q, want %q", got, want)
	}
}
