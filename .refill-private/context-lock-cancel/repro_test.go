package context_lock_cancel

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"astroplate-vault/internal/application"
	"astroplate-vault/internal/persistence"
)

func TestCanceledRequestEscapesContendedBatchLock(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	store, err := persistence.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	s := application.NewService(store)
	held := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- store.WithTx(context.Background(), func(*persistence.Tx) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	firstDone := make(chan error, 1)
	go func() {
		_, err := s.ManifestPreview(context.Background(), "blocked-batch", 0)
		firstDone <- err
	}()
	runtime.Gosched()
	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := s.ManifestPreview(ctx, "blocked-batch", 0)
		secondDone <- err
	}()
	runtime.Gosched()
	cancel()
	runtime.Gosched()
	blockedAfterCancel := false
	var canceledErr error
	select {
	case canceledErr = <-secondDone:
	default:
		blockedAfterCancel = true
	}
	close(release)
	if err = <-txDone; err != nil { t.Fatal(err) }
	<-firstDone
	if blockedAfterCancel { canceledErr = <-secondDone }
	if blockedAfterCancel || !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("TestCanceledRequestEscapesContendedBatchLock: canceled context remained blocked behind another request: err=%v", canceledErr)
	}
}
