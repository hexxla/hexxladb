package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/adapters/out/memory"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/app"
	"github.com/sploitzberg/go-hexagonal-architecture-template/internal/domain"
)

func TestService_HashMessage(t *testing.T) {
	t.Parallel()
	svc := app.New(memory.NewStore())
	got, err := svc.HashMessage(context.Background(), "hello")
	if err != nil {
		t.Fatalf("HashMessage: %v", err)
	}
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestService_StoreText_and_ListMessages(t *testing.T) {
	t.Parallel()
	svc := app.New(memory.NewStore())
	ctx := context.Background()
	if err := svc.StoreText(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.StoreText(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	msgs, err := svc.ListMessages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0] != "a" || msgs[1] != "b" {
		t.Fatalf("msgs = %#v", msgs)
	}
}

func TestService_StoreText_contentTooLarge(t *testing.T) {
	t.Parallel()
	svc := app.New(memory.NewStore())
	text := strings.Repeat("x", domain.MaxContentLen+1)
	err := svc.StoreText(context.Background(), text)
	if !errors.Is(err, domain.ErrContentTooLarge) {
		t.Fatalf("want ErrContentTooLarge, got %v", err)
	}
}

func TestService_HashMessage_invalid(t *testing.T) {
	t.Parallel()
	svc := app.New(memory.NewStore())
	_, err := svc.HashMessage(context.Background(), "   ")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}

func TestService_StoreText_invalid(t *testing.T) {
	t.Parallel()
	svc := app.New(memory.NewStore())
	err := svc.StoreText(context.Background(), "\n\t")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
}
